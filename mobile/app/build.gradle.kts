import java.util.Properties

val releaseTag = System.getenv("LEGATO_ANDROID_RELEASE_TAG")
val releaseVersion = releaseTag?.let { tag ->
    val match = Regex("v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)").matchEntire(tag)
        ?: throw GradleException("LEGATO_ANDROID_RELEASE_TAG must be a stable vMAJOR.MINOR.PATCH tag without leading zeros")
    val parts = match.groupValues.drop(1).map { it.toIntOrNull() }
    val major = parts[0]
    val minor = parts[1]
    val patch = parts[2]
    if (major == null || major > 2098 || minor == null || minor > 999 || patch == null || patch > 999) {
        throw GradleException("LEGATO_ANDROID_RELEASE_TAG requires major <= 2098 and minor/patch <= 999")
    }
    tag.removePrefix("v") to ((major + 1) * 1_000_000 + minor * 1_000 + patch)
}

// Official releases never consult local signing properties or use the debug key.
val releaseSigning: Properties? = if (releaseTag != null) {
    Properties().apply {
        mapOf(
            "storeFile" to "LEGATO_ANDROID_KEYSTORE",
            "storePassword" to "LEGATO_ANDROID_STORE_PASSWORD",
            "keyAlias" to "LEGATO_ANDROID_KEY_ALIAS",
            "keyPassword" to "LEGATO_ANDROID_KEY_PASSWORD",
        ).forEach { (property, variable) ->
            val value = System.getenv(variable)
            if (value.isNullOrEmpty()) throw GradleException("$variable must be nonempty for an official Android release")
            setProperty(property, value)
        }
        if (!file(getProperty("storeFile")).isFile) {
            throw GradleException("LEGATO_ANDROID_KEYSTORE must name an existing keystore file")
        }
    }
} else {
    // Local builds use the private key when available, otherwise the debug key.
    val signingPath = System.getenv("LEGATO_ANDROID_SIGNING")
    val signingFile = File(signingPath
        ?: "${System.getProperty("user.home")}/.config/legato/android/signing.properties")
    if (signingPath != null && !signingFile.isFile) {
        throw GradleException("LEGATO_ANDROID_SIGNING must name an existing signing properties file")
    }
    signingFile.takeIf { it.isFile }
        ?.let { file -> Properties().apply { file.inputStream().use(::load) } }
}

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

abstract class GenerateThemeAssets : DefaultTask() {
    @get:InputFile abstract val palette: RegularFileProperty
    @get:OutputDirectory abstract val output: DirectoryProperty

    @TaskAction fun generate() {
        val directory = output.get().asFile.apply { mkdirs() }
        palette.get().asFile.copyTo(directory.resolve("palettes.json"), overwrite = true)
    }
}

val themeAssets = tasks.register<GenerateThemeAssets>("generateThemeAssets") {
    palette.set(layout.projectDirectory.file("../../themes/palettes.json"))
    output.set(layout.buildDirectory.dir("generated/themeAssets"))
}

// AGP resolves the test source directory to a path, losing its task dependency.
tasks.matching { it.name.startsWith("process") && it.name.endsWith("UnitTestJavaRes") }.configureEach {
    dependsOn(themeAssets)
}

androidComponents.onVariants { variant ->
    variant.sources.assets?.addGeneratedSourceDirectory(themeAssets, GenerateThemeAssets::output)
}

android {
    namespace = "dev.legato.mobile"
    compileSdk = 36
    buildToolsVersion = "36.0.0"

    defaultConfig {
        // The same application ID as the Expo app it replaces, so an install
        // over it keeps the saved servers.
        applicationId = "dev.legato.mobile"
        minSdk = 29
        targetSdk = 36
        versionCode = releaseVersion?.second ?: 2
        versionName = releaseVersion?.first ?: "2.0.0"
    }

    signingConfigs {
        // The React Native template's public debug key, which signed every
        // local build of the Expo app. Signing with it lets this app install
        // over that one in place.
        getByName("debug") {
            storeFile = file("debug.keystore")
            storePassword = "android"
            keyAlias = "androiddebugkey"
            keyPassword = "android"
        }
        if (releaseSigning != null) {
            create("release") {
                storeFile = file(releaseSigning.getProperty("storeFile"))
                storePassword = releaseSigning.getProperty("storePassword")
                keyAlias = releaseSigning.getProperty("keyAlias")
                keyPassword = releaseSigning.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            // The private key when present: only builds signed with it can
            // update the app, and Legato can trust only its keys for approvals.
            signingConfig = signingConfigs.getByName(if (releaseSigning != null) "release" else "debug")
            if (releaseSigning == null) logger.warn("Release build signed with the public debug key: no ~/.config/legato/android/signing.properties")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    packaging {
        // BouncyCastle and JSpecify both carry this; the app needs neither.
        resources.excludes += "META-INF/versions/9/OSGI-INF/MANIFEST.MF"
        // BouncyCastle's data files for algorithms the app does not use.
        resources.excludes += "org/bouncycastle/**"
    }
    buildFeatures {
        compose = true
    }
    testOptions {
        unitTests.isReturnDefaultValues = false
    }
    // Package the canonical palettes, not an Android-maintained copy. No
    // repository files or network are needed at runtime.
    sourceSets.getByName("test").resources.srcDir(themeAssets.flatMap { it.output })
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2025.09.00")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("androidx.webkit:webkit:1.14.0")
    implementation("androidx.profileinstaller:profileinstaller:1.4.1")
    // Scanning a pairing QR code: the app's own camera (CameraX) and ZXing's
    // decoder, so nothing depends on Google Play services.
    implementation("androidx.camera:camera-camera2:1.4.2")
    implementation("androidx.camera:camera-lifecycle:1.4.2")
    implementation("androidx.camera:camera-view:1.4.2")
    implementation("com.google.zxing:core:3.5.3")
    // P-256 point arithmetic for pairing by phrase (SPAKE2).
    implementation("org.bouncycastle:bcprov-jdk18on:1.79")

    testImplementation("junit:junit:4.13.2")
    // Android's org.json is a stub on the JVM; tests use the real library.
    testImplementation("org.json:json:20250517")
}
