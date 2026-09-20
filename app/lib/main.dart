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
import 'cases/outbox.dart';
import 'core/design/app_theme.dart';
import 'core/design/tokens.dart';
import 'core/ui/surface.dart';
import 'db/database.dart';
import 'db/outbox_store.dart';
import 'db/reference_store.dart';
import 'db/schedule.dart';
import 'home.dart';
import 'packs/manifest.dart';
import 'packs/store.dart';

/// Адрес контура и ключ сборки задаются при сборке, а не литералом по
/// месту: адрес, вписанный во второй файл, расходится молча, а ключ,
/// зашитый в исходник, уезжает в открытый репозиторий.
///
/// ```
/// flutter build apk --dart-define=CURATOR_URL=… \
///   --dart-define=CURATOR_APP_KEY=… --dart-define=CURATOR_PACK_KEYS=…
/// ```
const _baseUrl = String.fromEnvironment(
  'CURATOR_URL',
  defaultValue: 'https://curator.psync.ru',
);
const _appKey = String.fromEnvironment('CURATOR_APP_KEY');

/// Открытые ключи, которыми подписаны выпуски наборов, — списком вида
/// `имя:база64,имя:база64`.
///
/// Приезжают в сборке, а не с сервера. Приедь ключ той же дверью, что и
/// набор, подпись перестала бы значить что-либо: подменивший набор по
/// дороге подменил бы и ключ. Ключей несколько намеренно — смена ключа не
/// должна делать прежние выпуски негодными все разом.
const _packKeys = String.fromEnvironment('CURATOR_PACK_KEYS');

Future<void> main() async {
  // Плагины поднимаются до первого обращения к ним: база открывается
  // раньше первого экрана, а без этой строки она отказала бы на старте.
  WidgetsFlutterBinding.ensureInitialized();

  final api = Api(
    baseUrl: Uri.parse(_baseUrl),
    appKey: _appKey,
    tokens: PrefsTokenStore(),
  );

  // База открывается до первого экрана: без неё повторение без сети
  // показывало бы пусто, а пустой список врач читает как «сегодня нечего
  // повторять» — то есть как правду.
  //
  // Не открылась — приложение всё равно поднимается, просто без работы
  // без сети. Отказ на старте из-за испорченного файла базы оставил бы
  // врача вообще без приложения, а лента и разбор от базы не зависят:
  // непонятое не применяется, но и не роняет остального.
  Schedule? schedule;
  ReferenceStore? reference;
  OutboxStore outboxStore = PrefsOutboxStore();
  try {
    final db = await openLocalDatabase();
    final store = DbOutboxStore(db);
    // Очередь, оставшаяся в настройках от прежней сборки, — это чей-то
    // месяц занятий. Переносится один раз: после переноса там пусто.
    await store.adopt(PrefsOutboxStore());
    outboxStore = store;
    schedule = Schedule(db);
    reference = ReferenceStore(db);
  } catch (error) {
    // Пишется в журнал, а не показывается врачу: показать ему нечего —
    // делать с этим он ничего не может, а приложение работает.
    debugPrint('местная база не открылась: $error');
  }

  runApp(
    CuratorApp(
      api: api,
      outbox: Outbox(api, outboxStore),
      packs: FilePackStore(),
      keys: TrustedKeys.parse(_packKeys),
      schedule: schedule,
      reference: reference,
    ),
  );
}

class CuratorApp extends StatelessWidget {
  const CuratorApp({
    super.key,
    required this.api,
    required this.outbox,
    required this.packs,
    required this.keys,
    required this.schedule,
    this.reference,
  });

  final Api api;
  final Outbox outbox;
  final PackStore packs;
  final TrustedKeys keys;

  /// Расписание повторения на устройстве. Пусто — значит, местная база не
  /// открылась, и работы без сети не будет; остальное работает.
  final Schedule? schedule;

  /// Справочник на устройстве. Пусто по той же причине.
  final ReferenceStore? reference;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Куратор',
      // Обе темы, а выбор — за системой. Тёмную нельзя не дать: врач
      // открывает приложение и ночью на дежурстве, и белый экран в
      // темноте — это не «непривычно», а больно.
      //
      // Плоскости в обеих разделяются волосяной границей, а не тенью:
      // тень допустима только у того, что физически висит над страницей.
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      home: StartScreen(
        api: api,
        outbox: outbox,
        packs: packs,
        keys: keys,
        schedule: schedule,
        reference: reference,
      ),
    );
  }
}

/// Первый экран.
///
/// Первый запуск не спрашивает у врача ничего: спрашивать имя у человека,
/// который ещё не понял, что ему предлагают, — верный способ его потерять.
/// Устройство заводится молча, а экран показывает, чем дело кончилось.
class StartScreen extends StatefulWidget {
  const StartScreen({
    super.key,
    required this.api,
    required this.outbox,
    required this.packs,
    required this.keys,
    required this.schedule,
    this.reference,
  });

  final Api api;
  final Outbox outbox;
  final PackStore packs;
  final TrustedKeys keys;

  /// Расписание повторения на устройстве. Пусто — значит, местная база не
  /// открылась, и работы без сети не будет; остальное работает.
  final Schedule? schedule;

  /// Справочник на устройстве. Пусто по той же причине.
  final ReferenceStore? reference;

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
    return FutureBuilder<ApiFailure?>(
      future: _ready,
      builder: (context, snapshot) {
        if (snapshot.connectionState != ConnectionState.done) {
          return const Scaffold(
            body: Center(child: CircularProgressIndicator()),
          );
        }
        final failure = snapshot.data;
        if (failure != null) {
          // Отказ показывается его словами: сервер пишет их по-русски и
          // говорит, что делать. «Ошибка 401» отправила бы врача
          // переустанавливать исправное приложение.
          return Scaffold(
            body: SafeArea(
              child: Padding(
                padding: Gap.screenH,
                child: _Failure(text: failure.message, onRetry: _again),
              ),
            ),
          );
        }
        // Заведение — это не экран, а порог: пройден, и врач сразу на
        // задачах. Отдельный экран «всё хорошо» здесь был бы препятствием
        // между человеком и тем, ради чего он поставил приложение.
        return Home(
          api: widget.api,
          outbox: widget.outbox,
          packs: widget.packs,
          keys: widget.keys,
          schedule: widget.schedule,
          reference: widget.reference,
        );
      },
    );
  }
}

class _Failure extends StatelessWidget {
  const _Failure({required this.text, required this.onRetry});

  final String text;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    // Пустое состояние, а не голый текст посреди экрана: у отказа на
    // старте обязан быть выход, иначе врач остаётся с сообщением и
    // кнопкой «назад», которой некуда вести.
    return EmptyState(
      icon: const Icon(Icons.cloud_off_outlined),
      title: 'Не вышло начать',
      description: text,
      action: OutlinedButton(onPressed: onRetry, child: const Text('Ещё раз')),
    );
  }
}
