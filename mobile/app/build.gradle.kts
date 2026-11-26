plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

val karouterDefaultUrl = (project.findProperty("KAROUTER_DEFAULT_URL") as String?)
    ?.trim()
    ?.trimEnd('/')
    ?.ifEmpty { "http://127.0.0.1:8031" }
    ?: "http://127.0.0.1:8031"

android {
    namespace = "com.karouter.mobile"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.karouter.mobile"
        minSdk = 26
        targetSdk = 34
        versionCode = 1
        versionName = "4.1.0"

        buildConfigField("String", "DEFAULT_BASE_URL", "\"$karouterDefaultUrl\"")
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            signingConfig = signingConfigs.getByName("debug")
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
        debug {
            isMinifyEnabled = false
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
        buildConfig = true
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.6")
    implementation("androidx.activity:activity-compose:1.9.2")
    implementation(platform("androidx.compose:compose-bom:2024.09.03"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
}
