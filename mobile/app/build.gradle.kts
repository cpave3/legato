plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "dev.legato.mobile"
    compileSdk = 36

    defaultConfig {
        // The same application ID as the Expo app it replaces, so an install
        // over it keeps the saved servers.
        applicationId = "dev.legato.mobile"
        minSdk = 26
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
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName("debug")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    buildFeatures {
        compose = true
    }
    testOptions {
        unitTests.isReturnDefaultValues = false
    }
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
    implementation("com.google.android.gms:play-services-code-scanner:16.1.0")
    // Play Services brings Fragment 1.0, which mishandles activity results.
    implementation("androidx.fragment:fragment:1.8.9")

    testImplementation("junit:junit:4.13.2")
    // Android's org.json is a stub on the JVM; tests use the real library.
    testImplementation("org.json:json:20250517")
}
