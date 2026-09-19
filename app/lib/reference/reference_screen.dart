/// Справочник: то, что врач открывает у постели больного.
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
import '../db/reference_store.dart';
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

  @override
  void initState() {
    super.initState();
    final store = widget.store;
    if (store == null) return;
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
    return Scaffold(
      appBar: AppBar(title: const Text('Справочник')),
      body: SafeArea(child: _body(context)),
    );
  }

  Widget _body(BuildContext context) {
    if (widget.store == null) {
      return const _Note(
        icon: Icons.sd_card_alert_outlined,
        text:
            'Справочник живёт на устройстве, а местная база не открылась. '
            'Перезапустите приложение',
      );
    }
    if (_loading) return const Center(child: CircularProgressIndicator());

    final theme = Theme.of(context);
    // Предлагается только то, чего нет или чей выпуск разошёлся: строка
    // «скачать» под уже скачанным читается как «оно не скачалось».
    final fresh = {for (final one in _mine) one.slug: one.version};
    final offers = _offered
        .where((one) => fresh[one.slug] != one.version)
        .toList();

    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (_note.isNotEmpty) ...[
            _Banner(text: _note),
            const SizedBox(height: 16),
          ],

          if (_mine.isEmpty && offers.isEmpty && _offerFailure.isEmpty)
            const _Note(
              icon: Icons.menu_book_outlined,
              text:
                  'Справочников пока нет. Они появятся здесь, '
                  'когда составитель их выпустит',
            ),

          if (_mine.isNotEmpty) ...[
            Text('На устройстве', style: theme.textTheme.titleSmall),
            const SizedBox(height: 10),
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
            if (_mine.isNotEmpty) const SizedBox(height: 24),
            Text('Можно скачать', style: theme.textTheme.titleSmall),
            const SizedBox(height: 10),
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
            const SizedBox(height: 16),
            Row(
              children: [
                Icon(
                  Icons.cloud_off_outlined,
                  size: 16,
                  color: theme.colorScheme.outline,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    _offerFailure,
                    style: theme.textTheme.labelMedium?.copyWith(
                      color: theme.colorScheme.outline,
                    ),
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
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
    final theme = Theme.of(context);
    return _Card(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _Head(source: source),
          const SizedBox(height: 10),
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
            _Progress(units: gotUnits, statements: gotStatements)
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
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                'Вышел новый выпуск',
                style: theme.textTheme.labelSmall?.copyWith(
                  color: theme.colorScheme.primary,
                ),
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
            _Progress(units: gotUnits, statements: gotStatements)
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
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Prose(source.title, style: theme.textTheme.titleMedium),
        if (source.edition.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 2),
            child: Text(
              source.edition,
              style: theme.textTheme.labelMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
      ],
    );
  }
}

class _Progress extends StatelessWidget {
  const _Progress({required this.units, required this.statements});

  final int units;
  final int statements;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const LinearProgressIndicator(),
        const SizedBox(height: 8),
        Text(
          // Двигающееся число вместо крутящегося кружка: закачка
          // справочника идёт десятки секунд, и кружок всё это время
          // неотличим от зависшего.
          'Прочитано: ${grouped(units)} и ${grouped(statements)}',
          style: theme.textTheme.labelMedium,
        ),
      ],
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 15, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 6),
        Text(text, style: theme.textTheme.labelMedium),
      ],
    );
  }
}

class _Card extends StatelessWidget {
  const _Card({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        // Волосяная граница, а не тень: карточка лежит на странице.
        border: Border.all(color: theme.colorScheme.outlineVariant),
        borderRadius: BorderRadius.circular(12),
      ),
      child: child,
    );
  }
}

class _Banner extends StatelessWidget {
  const _Banner({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(text, style: theme.textTheme.bodyMedium),
    );
  }
}

class _Note extends StatelessWidget {
  const _Note({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 48, horizontal: 8),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 40, color: theme.colorScheme.outline),
          const SizedBox(height: 12),
          Text(
            text,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
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
