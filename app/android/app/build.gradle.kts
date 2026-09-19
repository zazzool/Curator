import java.util.Properties

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "ru.psync.curator"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        // Идентификатор установки. Сменить его после первой выкладки нельзя:
        // магазин и устройство считают приложение с другим идентификатором
        // другим приложением, и обновление превратится в вторую установку
        // рядом с первой, со своей базой и потерянным прогрессом.
        applicationId = "ru.psync.curator"
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    // Ключ подписи берётся из key.properties, а сам файл и хранилище ключа
    // в репозитории не лежат и лежать не будут: ключ невосстановим, а
    // репозиторий открытый. Утёкший ключ подписи — это чужая сборка,
    // которую устройства примут за наше обновление.
    //
    // Без файла собирается отладочная подпись, и выкладывать такую сборку
    // нельзя. Отказа здесь нет намеренно: `flutter run` нужен всякому, кто
    // трогает приложение, а ключ — только тому, кто выкладывает.
    val keyProperties = Properties().apply {
        val file = rootProject.file("key.properties")
        if (file.exists()) file.inputStream().use { load(it) }
    }

    signingConfigs {
        if (keyProperties.getProperty("storeFile") != null) {
            create("release") {
                storeFile = file(keyProperties.getProperty("storeFile"))
                storePassword = keyProperties.getProperty("storePassword")
                keyAlias = keyProperties.getProperty("keyAlias")
                keyPassword = keyProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release")
                ?: signingConfigs.getByName("debug")
        }
    }
}

flutter {
    source = "../.."
}
