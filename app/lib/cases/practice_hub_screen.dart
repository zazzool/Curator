/// Раздел «Практика»: выбор режима, а не список задач.
///
/// Список задач показывать нельзя: врач заранее видел бы условие и
/// диагноз, и проверка знания превращалась бы в выбор знакомого. Поэтому
/// задачу выдаёт система — очередной из ленты или очередной на повтор.
///
/// **Экран не ждёт сети ни секунды.** Всё, что он показывает, лежит на
/// устройстве: сколько задач в наборах и сколько ждёт повторения. Витрина
/// наборов едет с сервера и появляется, когда доедет; не доехала — строки
/// каталога просто нет, а режимы работают. Кружок на весь экран здесь
/// стоять не может: он объявлял бы практику недоступной, пока ответит
/// сервер, — а у врача в метро все задачи лежат на устройстве.
///
/// Режимов два, и это не урезанный донор, а честный состав: марафон,
/// приёмный покой и экзамен — механики прогона, которых здесь нет.
/// Появятся — станут такими же плитками, и подпись у каждой скажет, что
/// внутри.
library;

import 'package:flutter/material.dart';

import '../api/client.dart';
import '../core/app_scope.dart';
import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/motion.dart';
import '../core/ui/quiet_progress.dart';
import '../core/ui/surface.dart';
import '../packs/download.dart';
import '../packs/shelf.dart';
import '../packs/shelf_screen.dart';
import '../settings/settings_screen.dart';
import '../text/plural.dart';
import 'practice_screen.dart';

class PracticeHubScreen extends StatefulWidget {
  const PracticeHubScreen({super.key});

  @override
  State<PracticeHubScreen> createState() => _PracticeHubScreenState();
}

class _PracticeHubScreenState extends State<PracticeHubScreen> {
  /// Задач в наборах на устройстве. `null` — ещё не считали, и числа под
  /// заголовком нет: «0 задач» на непосчитанном врач принял бы за правду.
  int? _onDevice;

  /// Сколько задач ждёт повторения сегодня. `null` — местной базы нет, и
  /// повторения без сети не будет.
  int? _due;

  /// Витрина наборов. Пусто — не доехала или в ней нечего предлагать.
  List<ShelfPack> _catalog = const [];

  bool _reading = true;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _read());
  }

  Future<void> _read() async {
    if (!mounted) return;
    setState(() => _reading = true);
    final scope = AppScope.of(context);

    var onDevice = 0;
    for (final slug in await scope.packs.installed()) {
      onDevice += (await scope.packs.have(slug)).length;
    }
    final schedule = scope.schedule;
    final due = schedule == null
        ? null
        : (await schedule.due(DateTime.now(), limit: 1000)).length;

    var catalog = const <ShelfPack>[];
    try {
      catalog = await Shelf(scope.api, scope.packs).list();
    } on ApiFailure {
      // Молча: витрина — не то, ради чего сюда пришли, и отказ сети
      // здесь не новость, а обычное состояние в метро.
    }

    if (!mounted) return;
    setState(() {
      _onDevice = onDevice;
      _due = due;
      _catalog = catalog;
      _reading = false;
    });
  }

  Future<void> _start(PracticeSource source) async {
    final scope = AppScope.of(context);
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => PracticeScreen(
          api: scope.api,
          outbox: scope.outbox,
          packs: scope.packs,
          schedule: scope.schedule,
          source: source,
        ),
      ),
    );
    // Вернулся с занятия — сроки повторения и очередь другие. Перечитать
    // надо молча: врач вышел из задачи, а не попросил обновить.
    await _read();
  }

  void _openPacks() {
    final scope = AppScope.of(context);
    Navigator.of(context)
        .push(
          MaterialPageRoute<void>(
            builder: (_) => ShelfScreen(
              shelf: Shelf(scope.api, scope.packs),
              download: Download(scope.api, scope.packs, scope.keys),
              store: scope.packs,
            ),
          ),
        )
        // Из каталога возвращаются со скачанным набором: число задач под
        // заголовком обязано это показать.
        .then((_) => _read());
  }

  @override
  Widget build(BuildContext context) {
    final onDevice = _onDevice;
    final due = _due ?? 0;

    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: ListView(
          padding: Gap.screenH.add(const EdgeInsets.only(bottom: Gap.xxl)),
          children: [
            // Заголовок первым: это то, что читают раньше всего, и дорога
            // в каталог, как бы ни была важна, стоит под ним, а не над.
            ScreenHeader(
              title: 'Практика',
              subtitle: onDevice == null
                  ? null
                  : withPlural(
                      onDevice,
                      'задача в наборе',
                      'задачи в наборе',
                      'задач в наборе',
                    ),
              // Значка «Наборы» в шапке нет: дорога в каталог — строкой
              // под заголовком, а не значком. Значок находят только те,
              // кто уже знает о витрине. Значок настроек — другое дело:
              // он один и тот же на каждом корневом экране.
              actions: const [SettingsHeaderAction()],
            ),
            UpdatingLine(updating: _reading, label: 'Задачи обновляются'),
            if (_catalog.isNotEmpty)
              _CatalogBanner(packs: _catalog, onTap: _openPacks),
            _ModeTile(
              icon: Icons.school_outlined,
              // У донора этот режим зовётся «Случайная задача»: там
              // задачу выбирает подбор из установленного набора. Здесь
              // очередь задаёт лента — что решать дальше, — и назвать её
              // случайной значило бы обещать жребий там, где его нет.
              title: 'Новая задача',
              subtitle: 'Следующая по вашей очереди',
              primary: true,
              onTap: () => _start(PracticeSource.feed),
            ),
            _ModeTile(
              icon: Icons.replay_outlined,
              title: 'Повторение',
              // Подпись живая: число ждущих сегодня. Оно и есть ответ на
              // вопрос «заходить ли сюда сейчас».
              subtitle: due == 0
                  ? 'Появится после первой пройденной задачи'
                  : 'Ждут повторения: $due',
              tone: _ModeTone.success,
              enabled: due > 0,
              onTap: () => _start(PracticeSource.review),
            ),
          ],
        ),
      ),
    );
  }
}

/// Дорога в каталог наборов: одна строка под заголовком.
class _CatalogBanner extends StatelessWidget {
  const _CatalogBanner({required this.packs, required this.onTap});

  final List<ShelfPack> packs;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final absent = packs.where((one) => !one.isInstalled).length;

    return Padding(
      // Сверху отступа нет: шапка экрана заканчивается своим полем, и два
      // отступа подряд отбивали бы строку от заголовка сильнее, чем от
      // того, что идёт ниже.
      padding: const EdgeInsets.only(bottom: Gap.md),
      child: Surface(
        padding: const EdgeInsets.symmetric(
          horizontal: Gap.lg,
          vertical: Gap.md,
        ),
        onTap: onTap,
        child: Row(
          children: [
            Icon(Icons.inventory_2_outlined, size: 20, color: p.inkMuted),
            const SizedBox(width: Gap.md),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Каталог наборов',
                    style: AppType.bodyStrong.copyWith(color: p.ink),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    absent == 0
                        ? '${withPlural(packs.length, 'набор', 'набора', 'наборов')}'
                              ' · все уже на устройстве'
                        : '${withPlural(packs.length, 'набор', 'набора', 'наборов')}'
                              ' · $absent ещё не у вас',
                    style: AppType.caption.copyWith(color: p.inkFaint),
                  ),
                ],
              ),
            ),
            Icon(Icons.chevron_right, size: 18, color: p.inkFaint),
          ],
        ),
      ),
    );
  }
}

/// Цвет плитки режима.
enum _ModeTone { accent, success }

/// Пункт меню режима.
class _ModeTile extends StatelessWidget {
  const _ModeTile({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.onTap,
    this.tone = _ModeTone.accent,
    this.primary = false,
    this.enabled = true,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback onTap;
  final _ModeTone tone;

  /// Главный режим: значок залит градиентом, а не подложкой. Если врач не
  /// знает, с чего начать, взгляд сам приводит его сюда.
  final bool primary;

  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final (color, soft) = switch (tone) {
      _ModeTone.accent => (p.accent, p.accentSoft),
      _ModeTone.success => (p.success, p.successSoft),
    };

    return Padding(
      padding: const EdgeInsets.only(bottom: Gap.md),
      child: Surface(
        onTap: enabled
            ? () {
                Haptics.tick();
                onTap();
              }
            : null,
        radius: Radii.heroAll,
        color: primary && enabled ? p.accentSoft : null,
        borderColor: primary && enabled
            ? p.accent.withValues(alpha: 0.18)
            : null,
        padding: const EdgeInsets.all(Gap.lg),
        child: Row(
          children: [
            _ModeBadge(
              icon: icon,
              color: enabled ? color : p.inkFaint,
              background: enabled ? soft : p.surfaceMuted,
              gradient: primary && enabled ? p.energyGradient : null,
            ),
            const SizedBox(width: Gap.lg),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: AppType.titleM.copyWith(
                      color: enabled ? p.ink : p.inkFaint,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    subtitle,
                    style: AppType.caption.copyWith(color: p.inkFaint),
                  ),
                ],
              ),
            ),
            if (enabled) ...[
              const SizedBox(width: Gap.sm),
              Icon(Icons.arrow_forward_rounded, size: 18, color: p.inkFaint),
            ],
          ],
        ),
      ),
    );
  }
}

/// Квадратный значок режима со скруглением «сквиркл».
class _ModeBadge extends StatelessWidget {
  const _ModeBadge({
    required this.icon,
    required this.color,
    required this.background,
    this.gradient,
  });

  final IconData icon;
  final Color color;
  final Color background;
  final Gradient? gradient;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Container(
      width: 48,
      height: 48,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: gradient == null ? background : null,
        gradient: gradient,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Icon(
        icon,
        size: 22,
        color: gradient == null ? color : p.accentInk,
      ),
    );
  }
}
