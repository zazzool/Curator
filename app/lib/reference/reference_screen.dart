/// Раздел «Теория»: то, что врач открывает у постели больного.
///
/// # Почему это отдельный раздел, а не подсказка внутри задачи
///
/// Врач приходит сюда не из задачи. Он приходит из отделения, где нужно
/// вспомнить критерии рубрики, и приходит без сети. Справочник, доступный
/// только изнутри разбора, в этот момент не существует.
///
/// # Ничего не качается само
///
/// Закачку начинает врач и начинает, зная размер: справочник это
/// мегабайты, а телефон бывает на дорогом тарифе. Молча потраченный
/// трафик — та же молчаливая трата, что и молча выброшенные данные.
///
/// # Список для скачивания приезжает с сервера, читается — с устройства
///
/// Эти два списка нарочно разведены. Нет сети — верхний список пуст, а
/// нижний, скачанный, открывается как ни в чём не бывало: ради этого
/// справочник и переносился.
library;

import 'package:flutter/material.dart';

import '../api/client.dart';
import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/quiet_progress.dart';
import '../core/ui/search_field.dart';
import '../core/ui/surface.dart';
import '../db/reference_store.dart';
import '../settings/settings_screen.dart';
import '../text/plural.dart';
import '../text/prose.dart';
import 'model.dart';
import 'source_screen.dart';
import 'sync.dart';

class ReferenceScreen extends StatefulWidget {
  const ReferenceScreen({super.key, required this.api, this.store});

  final Api api;

  /// Справочник на устройстве. Пусто — местная база не открылась, и
  /// справочника не будет вовсе: он весь про чтение без сети.
  final ReferenceStore? store;

  @override
  State<ReferenceScreen> createState() => _ReferenceScreenState();
}

class _ReferenceScreenState extends State<ReferenceScreen> {
  List<RefSource> _mine = const [];
  List<RefSource> _offered = const [];

  bool _loading = true;

  /// Отказ списка с сервера. Показывается строкой, а не вместо экрана:
  /// скачанное читается и без сети, и прятать его из-за отказа сети —
  /// ровно та поломка, ради которой справочник переносили.
  String _offerFailure = '';

  /// Что качается прямо сейчас и сколько уже прочитано.
  String _busy = '';
  int _gotUnits = 0;
  int _gotStatements = 0;

  /// Чем кончилась последняя закачка. Держится до следующего действия:
  /// сообщение, стёртое обновлением списка, врач прочесть не успевает.
  String _note = '';

  /// Закачка заводится только при открытой базе: класть выпуск некуда,
  /// и «скачать» на экране без базы было бы кнопкой в никуда.
  ReferenceSync? _sync;

  /// Поиск по единицам ВСЕХ скачанных источников разом.
  ///
  /// Место поиска — там, где источники перечислены вместе. Пока он жил
  /// только внутри источника, дорога к пункту приказа шла через раздел
  /// про диагнозы: врач должен был сперва угадать, в каком источнике
  /// искать, — а он приходит с кодом из карты или со словом из головы, и
  /// не обязан знать, кто его выпустил.
  final _query = TextEditingController();
  List<(RefSource, RefUnit)> _hits = const [];

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    final store = widget.store;
    if (store == null) {
      // Читать нечего и не из чего: отметка «разделы обновляются» над
      // сообщением «теория недоступна» крутилась бы вечно и обещала
      // обновление, которого не будет.
      _loading = false;
      return;
    }
    _sync = ReferenceSync(widget.api, store);
    _load();
  }

  Future<void> _load({String keepNote = ''}) async {
    setState(() {
      _loading = true;
      _note = keepNote;
    });
    final mine = await widget.store!.sources();
    var offered = const <RefSource>[];
    var failure = '';
    try {
      offered = await _sync!.available();
    } on ApiFailure catch (error) {
      failure = error.message;
    }
    if (!mounted) return;
    setState(() {
      _mine = mine;
      _offered = offered;
      _offerFailure = failure;
      _loading = false;
    });
  }

  Future<void> _search(String raw) async {
    final query = raw.trim();
    if (query.isEmpty) {
      setState(() => _hits = const []);
      return;
    }
    final store = widget.store!;
    final hits = <(RefSource, RefUnit)>[];
    // По источникам подряд, в порядке списка: у каждого своя выдача, и
    // сводить их в один ранжированный список нечем — совпадение по началу
    // метки в одном источнике не сравнимо с совпадением в другом.
    for (final source in _mine) {
      for (final unit in await store.search(source.slug, query, limit: 20)) {
        hits.add((source, unit));
      }
    }
    if (!mounted) return;
    // Набранное могло смениться, пока шла выборка: показывать выдачу по
    // прежнему запросу нельзя — врач читал бы ответ на чужой вопрос.
    if (_query.text.trim() != query) return;
    setState(() => _hits = hits);
  }

  Future<void> _pull(RefSource source, {bool force = false}) async {
    setState(() {
      _busy = source.slug;
      _gotUnits = 0;
      _gotStatements = 0;
      _note = '';
    });
    var note = '';
    try {
      final report = await _sync!.pull(
        source,
        force: force,
        onProgress: (units, statements) {
          if (!mounted) return;
          setState(() {
            _gotUnits = units;
            _gotStatements = statements;
          });
        },
      );
      note = report.skipped
          ? '«${source.title}» и так последнего выпуска'
          : '«${source.title}» на устройстве: '
                '${counted(report.units, source.unitWord).toLowerCase()}, '
                '${counted(report.statements, source.statementWord).toLowerCase()}';
      // Выброшенное считается и называется: молча потерянная половина
      // справочника выглядит как «столько в нём и было».
      if (report.dropped > 0) {
        note +=
            '. Не разобрано строк: ${report.dropped} — '
            'обновите приложение, они из новой версии справочника';
      }
    } on ApiFailure catch (error) {
      note = error.message;
    }
    if (!mounted) return;
    setState(() => _busy = '');
    await _load(keepNote: note);
  }

  Future<void> _forget(RefSource source) async {
    await widget.store!.forget(source.slug);
    await _load(keepNote: '«${source.title}» убран с устройства');
  }

  @override
  Widget build(BuildContext context) {
    // Шапка в потоке содержимого, как на прочих корневых экранах: полоса
    // `AppBar` стоила бы вертикальной полосы там, где её занимает список.
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: Gap.screenH,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ScreenHeader(
                title: 'Теория',
                // Счёт называется только тогда, когда он посчитан: «0
                // разделов» на непрочитанном диске врач принял бы за
                // правду.
                subtitle: _loading && _mine.isEmpty
                    ? null
                    : withPlural(_mine.length, 'раздел', 'раздела', 'разделов'),
                // Значок настроек стоит на каждом корневом экране и ведёт
                // в одно место с одного и того же значка.
                actions: const [SettingsHeaderAction()],
              ),
              // Обновление — тонкой отметкой под шапкой: список, лежащий
              // на диске, читается всё это время как обычно.
              UpdatingLine(updating: _loading, label: 'Разделы обновляются'),
              // Поля нет там, где искать нечего: пустое поле обещает
              // выдачу, которой не будет.
              if (_mine.isNotEmpty) ...[
                AppSearchField(
                  controller: _query,
                  hintText: 'Метка или название',
                  onChanged: _search,
                  onClear: _query.text.isEmpty
                      ? null
                      : () {
                          _query.clear();
                          _search('');
                        },
                ),
                const SizedBox(height: Gap.lg),
              ],
              Expanded(child: _body(context)),
            ],
          ),
        ),
      ),
    );
  }

  Widget _body(BuildContext context) {
    if (widget.store == null) {
      return const _Note(
        icon: Icons.sd_card_alert_outlined,
        title: 'Теория недоступна',
        text:
            'Она живёт на устройстве, а местная база не открылась. '
            'Перезапустите приложение',
      );
    }
    // Пока диск читается, экран не утверждает ничего: отметка обновления
    // стоит выше, а кружка на весь экран здесь быть не может — справочник
    // весь про чтение без сети.
    if (_loading && _mine.isEmpty) return const SizedBox.shrink();

    // Выдача занимает место списка разделов, а не тянется под ним: два
    // списка сразу читались бы как один.
    if (_query.text.trim().isNotEmpty) return _found(context);

    final p = context.palette;
    // Предлагается только то, чего нет или чей выпуск разошёлся: строка
    // «скачать» под уже скачанным читается как «оно не скачалось».
    final fresh = {for (final one in _mine) one.slug: one.version};
    final offers = _offered
        .where((one) => fresh[one.slug] != one.version)
        .toList();

    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.only(bottom: Gap.xxl),
        children: [
          if (_note.isNotEmpty) ...[
            NoteBanner(text: _note),
            const SizedBox(height: Gap.lg),
          ],

          if (_mine.isEmpty && offers.isEmpty && _offerFailure.isEmpty)
            const _Note(
              icon: Icons.menu_book_outlined,
              title: 'Разделов пока нет',
              text:
                  'Теория появится вместе с источниками, которые выпустит '
                  'составитель',
            ),

          if (_mine.isNotEmpty) ...[
            const SectionLabel('На устройстве'),
            for (final one in _mine)
              _MineTile(
                source: one,
                stale:
                    fresh[one.slug] != null &&
                    offers.any((o) => o.slug == one.slug),
                onOpen: () => _open(one),
                onUpdate: () {
                  final offer = _offered.firstWhere(
                    (o) => o.slug == one.slug,
                    orElse: () => one,
                  );
                  _pull(offer, force: true);
                },
                onForget: () => _forget(one),
                busy: _busy == one.slug,
                gotUnits: _gotUnits,
                gotStatements: _gotStatements,
              ),
          ],

          if (offers.isNotEmpty) ...[
            if (_mine.isNotEmpty) const SizedBox(height: Gap.xl),
            const SectionLabel('Можно скачать'),
            for (final one in offers)
              _OfferTile(
                source: one,
                busy: _busy == one.slug,
                gotUnits: _gotUnits,
                gotStatements: _gotStatements,
                onPull: () => _pull(one),
              ),
          ],

          if (_offerFailure.isNotEmpty) ...[
            const SizedBox(height: Gap.lg),
            Row(
              children: [
                Icon(Icons.cloud_off_outlined, size: 16, color: p.inkFaint),
                const SizedBox(width: Gap.sm),
                Expanded(
                  child: Text(
                    _offerFailure,
                    style: AppType.caption.copyWith(color: p.inkFaint),
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  Widget _found(BuildContext context) {
    if (_hits.isEmpty) {
      return const _Note(
        icon: Icons.search_off_outlined,
        title: 'Ничего не нашлось',
        text: 'Попробуйте метку целиком или слово из названия',
      );
    }

    return ListView.separated(
      padding: const EdgeInsets.only(bottom: Gap.xxl),
      itemCount: _hits.length,
      separatorBuilder: (context, _) => rowDivider(context, indent: 30),
      itemBuilder: (context, i) {
        final (source, unit) = _hits[i];
        return UnitRow(
          unit: unit,
          statementWord: source.statementWord,
          // Путь показывается всегда: в общей выдаче одна метка ничего не
          // говорит о том, откуда она, — а источников на устройстве
          // несколько.
          showPath: true,
          onTap: () => Navigator.of(context).push(
            MaterialPageRoute<void>(
              builder: (_) =>
                  UnitScreen(store: widget.store!, source: source, unit: unit),
            ),
          ),
        );
      },
    );
  }

  void _open(RefSource source) {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => SourceScreen(store: widget.store!, source: source),
      ),
    );
  }
}

/// Скачанный источник.
class _MineTile extends StatelessWidget {
  const _MineTile({
    required this.source,
    required this.stale,
    required this.onOpen,
    required this.onUpdate,
    required this.onForget,
    required this.busy,
    required this.gotUnits,
    required this.gotStatements,
  });

  final RefSource source;

  /// На сервере лежит другой выпуск.
  final bool stale;

  final VoidCallback onOpen;
  final VoidCallback onUpdate;
  final VoidCallback onForget;
  final bool busy;
  final int gotUnits;
  final int gotStatements;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return _Card(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _Head(source: source),
          const SizedBox(height: Gap.md),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _Fact(
                icon: Icons.account_tree_outlined,
                text: counted(source.units, source.unitWord),
              ),
              _Fact(
                icon: Icons.checklist_outlined,
                text: counted(source.statements, source.statementWord),
              ),
              if (source.syncedAt > 0)
                _Fact(
                  icon: Icons.history,
                  text: 'Обновлён ${_day(source.syncedAt)}',
                ),
            ],
          ),
          const SizedBox(height: 12),
          if (busy)
            _Progress(
              units: gotUnits,
              statements: gotStatements,
              unitWord: source.unitWord,
              statementWord: source.statementWord,
            )
          else
            Row(
              children: [
                FilledButton(onPressed: onOpen, child: const Text('Открыть')),
                const SizedBox(width: 8),
                if (stale)
                  OutlinedButton(
                    onPressed: onUpdate,
                    child: const Text('Обновить'),
                  ),
                const Spacer(),
                IconButton(
                  onPressed: onForget,
                  tooltip: 'Убрать с устройства',
                  icon: const Icon(Icons.delete_outline),
                ),
              ],
            ),
          if (stale && !busy)
            Padding(
              padding: const EdgeInsets.only(top: Gap.sm),
              child: Text(
                'Вышел новый выпуск',
                style: AppType.caption.copyWith(color: p.accent),
              ),
            ),
        ],
      ),
    );
  }
}

/// Источник, предлагаемый к скачиванию.
class _OfferTile extends StatelessWidget {
  const _OfferTile({
    required this.source,
    required this.busy,
    required this.gotUnits,
    required this.gotStatements,
    required this.onPull,
  });

  final RefSource source;
  final bool busy;
  final int gotUnits;
  final int gotStatements;
  final VoidCallback onPull;

  @override
  Widget build(BuildContext context) {
    return _Card(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _Head(source: source),
          const SizedBox(height: 10),
          // Размер закачки назван до неё, а не после: трафик тратит врач,
          // и решать, тратить ли, тоже ему.
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _Fact(
                icon: Icons.account_tree_outlined,
                text: counted(source.units, source.unitWord),
              ),
              _Fact(
                icon: Icons.checklist_outlined,
                text: counted(source.statements, source.statementWord),
              ),
            ],
          ),
          const SizedBox(height: 12),
          if (busy)
            _Progress(
              units: gotUnits,
              statements: gotStatements,
              unitWord: source.unitWord,
              statementWord: source.statementWord,
            )
          else
            FilledButton.tonalIcon(
              onPressed: onPull,
              icon: const Icon(Icons.download_outlined, size: 18),
              label: const Text('Скачать'),
            ),
        ],
      ),
    );
  }
}

class _Head extends StatelessWidget {
  const _Head({required this.source});

  final RefSource source;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Prose(source.title, style: AppType.titleS.copyWith(color: p.ink)),
        if (source.edition.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: Gap.xs),
            child: Text(
              source.edition,
              style: AppType.caption.copyWith(color: p.inkFaint),
            ),
          ),
      ],
    );
  }
}

class _Progress extends StatelessWidget {
  const _Progress({
    required this.units,
    required this.statements,
    required this.unitWord,
    required this.statementWord,
  });

  final int units;
  final int statements;
  final String unitWord;
  final String statementWord;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const LinearProgressIndicator(),
        const SizedBox(height: Gap.sm),
        Text(
          // Двигающееся число вместо крутящегося кружка: закачка
          // справочника идёт десятки секунд, и кружок всё это время
          // неотличим от зависшего.
          //
          // Числа названы словами источника, а не голыми: «954 и 765»
          // заставляет врача гадать, что из них что, а до конца закачки
          // проверить нечем.
          '${counted(units, unitWord)}, '
          '${counted(statements, statementWord).toLowerCase()}',
          style: AppType.caption.copyWith(color: p.inkMuted),
        ),
      ],
    );
  }
}

/// Число со значком: сколько в источнике единиц и положений.
///
/// Таблетка из общего набора, а не голый ряд «значок + текст»: те же
/// числа под задачей показаны таблетками, и два вида одной и той же
/// мелочи на соседних экранах читаются как два приложения.
class _Fact extends StatelessWidget {
  const _Fact({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) => Pill(label: text, icon: Icon(icon));
}

/// Карточка источника.
///
/// Плоскость из общего набора, а не своя: карточка, свёрстанная по месту,
/// разъезжается с остальными по радиусу и отступам — и разъезд этот врач
/// видит раньше нас.
class _Card extends StatelessWidget {
  const _Card({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: Gap.md),
    child: Surface(child: child),
  );
}

/// Пустое состояние справочника.
///
/// Заголовок и пояснение врозь: одной длинной строкой экран читается как
/// отказ, хотя чаще это исправный случай — справочников просто ещё нет.
class _Note extends StatelessWidget {
  const _Note({required this.icon, required this.title, required this.text});

  final IconData icon;
  final String title;
  final String text;

  @override
  Widget build(BuildContext context) =>
      EmptyState(icon: Icon(icon), title: title, description: text);
}

/// День закачки словами, без часов.
///
/// Часы здесь не нужны: врач спрашивает «свежий ли», а не «в котором часу».
String _day(int millis) {
  final at = DateTime.fromMillisecondsSinceEpoch(millis);
  final d = at.day.toString().padLeft(2, '0');
  final m = at.month.toString().padLeft(2, '0');
  return '$d.$m.${at.year}';
}
