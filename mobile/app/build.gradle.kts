import java.util.Properties

// The release key, kept outside the repository: a properties file with
// storeFile, storePassword, keyAlias and keyPassword. Its path is
// $LEGATO_ANDROID_SIGNING, else ~/.config/legato/android/signing.properties.
// Without it, release builds are signed with the public debug key below.
val releaseSigning: Properties? = (System.getenv("LEGATO_ANDROID_SIGNING")
    ?: "${System.getProperty("user.home")}/.config/legato/android/signing.properties")
    .let(::File).takeIf { it.isFile }
    ?.let { file -> Properties().apply { file.inputStream().use(::load) } }

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

androidComponents.onVariants { variant ->
    variant.sources.assets?.addGeneratedSourceDirectory(themeAssets, GenerateThemeAssets::output)
}

android {
    namespace = "dev.legato.mobile"
    compileSdk = 36

    defaultConfig {
        // The same application ID as the Expo app it replaces, so an install
        // over it keeps the saved servers.
        applicationId = "dev.legato.mobile"
        minSdk = 29
        targetSdk = 36
        versionCode = 2
        versionName = "2.0.0"
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
