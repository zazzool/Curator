/// Куратор: приложение обучающегося.
///
/// Сейчас здесь подъём, дверь к серверу и один экран: остальное приезжает
/// по этапам сквозного пути. Пустое, но поднимающееся приложение лучше
/// заготовки на будущее — по нему сразу видно, что сборка собирается,
/// встаёт на устройство и достучивается до сервера.
///
/// Расчёты, которые обязаны сходиться с сервером, уже здесь и сверены с
/// общими эталонами: опыт, уровни, повторение по интервалам, величины,
/// знаки и словарь событий. Они появились первыми не случайно — именно они
/// расходятся молча, и починить разошедшееся на руках у врача нечем.
library;

import 'package:flutter/material.dart';

import 'api/client.dart';
import 'api/token_store.dart';

/// Адрес контура и ключ сборки задаются при сборке, а не литералом по
/// месту: адрес, вписанный во второй файл, расходится молча, а ключ,
/// зашитый в исходник, уезжает в открытый репозиторий.
///
/// ```
/// flutter build apk --dart-define=CURATOR_URL=… --dart-define=CURATOR_APP_KEY=…
/// ```
const _baseUrl = String.fromEnvironment(
  'CURATOR_URL',
  defaultValue: 'https://curator.psync.ru',
);
const _appKey = String.fromEnvironment('CURATOR_APP_KEY');

void main() {
  runApp(
    CuratorApp(
      api: Api(
        baseUrl: Uri.parse(_baseUrl),
        appKey: _appKey,
        tokens: PrefsTokenStore(),
      ),
    ),
  );
}

class CuratorApp extends StatelessWidget {
  const CuratorApp({super.key, required this.api});

  final Api api;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Куратор',
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF2F5D62)),
        // Плоскости разделяются волосяной границей, а не тенью: тень
        // допустима только у того, что физически висит над страницей.
        cardTheme: const CardThemeData(elevation: 0),
        appBarTheme: const AppBarTheme(elevation: 0, scrolledUnderElevation: 0),
      ),
      home: StartScreen(api: api),
    );
  }
}

/// Первый экран.
///
/// Первый запуск не спрашивает у врача ничего: спрашивать имя у человека,
/// который ещё не понял, что ему предлагают, — верный способ его потерять.
/// Устройство заводится молча, а экран показывает, чем дело кончилось.
class StartScreen extends StatefulWidget {
  const StartScreen({super.key, required this.api});

  final Api api;

  @override
  State<StartScreen> createState() => _StartScreenState();
}

class _StartScreenState extends State<StartScreen> {
  // Отказ — это состояние экрана, а не отклонённое обещание. Обещание,
  // которое некому выслушать хотя бы мгновение, Flutter считает
  // необработанным и валит им проверку; да и читается «ничего или отказ»
  // яснее, чем «успех против исключения».
  late Future<ApiFailure?> _ready;

  @override
  void initState() {
    super.initState();
    _ready = _enroll();
  }

  Future<ApiFailure?> _enroll() async {
    try {
      await widget.api.ensureEnrolled(platform: 'android');
      return null;
    } on ApiFailure catch (failure) {
      return failure;
    } catch (_) {
      return ApiFailure('Не вышло начать. Попробуйте ещё раз');
    }
  }

  void _again() => setState(() {
    _ready = _enroll();
  });

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Куратор')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: FutureBuilder<ApiFailure?>(
            future: _ready,
            builder: (context, snapshot) {
              if (snapshot.connectionState != ConnectionState.done) {
                return const CircularProgressIndicator();
              }
              final failure = snapshot.data;
              if (failure != null) {
                // Отказ показывается его словами: сервер пишет их
                // по-русски и говорит, что делать. «Ошибка 401» отправила
                // бы врача переустанавливать исправное приложение.
                return _Failure(text: failure.message, onRetry: _again);
              }
              return const Text(
                'Приложение собирается. Задачи появятся здесь, '
                'когда доедут экраны разбора.',
                textAlign: TextAlign.center,
              );
            },
          ),
        ),
      ),
    );
  }
}

class _Failure extends StatelessWidget {
  const _Failure({required this.text, required this.onRetry});

  final String text;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(text, textAlign: TextAlign.center),
        const SizedBox(height: 16),
        OutlinedButton(onPressed: onRetry, child: const Text('Ещё раз')),
      ],
    );
  }
}
