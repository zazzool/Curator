/// Экран наборов.
///
/// # Почему здесь нет кнопки «Купить»
///
/// Приём платежей в приложении ещё не сделан: право на набор оформляет
/// оператор. Кнопка, которая ничего не покупает, хуже её отсутствия —
/// врач нажмёт и решит, что приложение сломалось. Поэтому закрытый набор
/// показывает цену и говорит словами, что делать; кнопка появится вместе с
/// приёмом платежей, а не раньше.
library;

import 'package:flutter/material.dart';

import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/bars.dart';
import '../core/ui/quiet_progress.dart';
import '../core/ui/surface.dart';
import '../api/client.dart';
import 'download.dart';
import 'manifest.dart';
import 'shelf.dart';
import 'store.dart';

class ShelfScreen extends StatefulWidget {
  const ShelfScreen({
    super.key,
    required this.shelf,
    required this.download,
    required this.store,
  });

  /// Витрина и закачка приходят готовыми, а не собираются здесь из двери и
  /// ключей. Так же сюда приходят дверь и очередь разборов: экран не
  /// должен знать, откуда берутся ключи подписи, а проверке не должно
  /// требоваться сети, чтобы показать четыре состояния набора.
  final Shelf shelf;
  final Download download;
  final PackStore store;

  @override
  State<ShelfScreen> createState() => _ShelfScreenState();
}

class _ShelfScreenState extends State<ShelfScreen> {
  List<ShelfPack> _packs = [];
  bool _loading = true;
  String? _failure;

  /// Набор, который качается прямо сейчас, и сколько задач уже приехало.
  String? _busy;
  int _done = 0;
  int _total = 0;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _failure = null;
    });
    try {
      final packs = await widget.shelf.list();
      if (!mounted) return;
      setState(() {
        _packs = packs;
        _loading = false;
      });
    } on ApiFailure catch (failure) {
      if (!mounted) return;
      setState(() {
        _failure = failure.message;
        _loading = false;
      });
    }
  }

  Future<void> _download(ShelfPack pack) async {
    setState(() {
      _busy = pack.slug;
      _done = 0;
      _total = pack.cases;
      _failure = null;
    });

    // Отказ запоминается здесь, а не кладётся сразу в состояние экрана:
    // следом идёт обновление витрины, а оно начинается с очистки отказа —
    // и врач увидел бы, что ничего не произошло, вместо причины.
    String? failed;
    try {
      await widget.download.run(
        pack.slug,
        onProgress: (done, total) {
          if (!mounted) return;
          setState(() {
            _done = done;
            _total = total;
          });
        },
      );
    } on ApiFailure catch (failure) {
      failed = failure.message;
    } on SignatureFailure catch (failure) {
      failed = failure.message;
    } on ContentFailure catch (failure) {
      failed = failure.message;
    } finally {
      if (mounted) setState(() => _busy = null);
    }

    await _load();
    if (failed != null && mounted) {
      setState(() => _failure = failed);
    }
  }

  Future<void> _remove(ShelfPack pack) async {
    await widget.store.remove(pack.slug);
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: Gap.screenH,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ScreenHeader(
                // «Каталог», а не «Магазин» и не «Премиум»: врач приходит
                // сюда за задачами, а не за покупкой, и большая часть
                // наборов бесплатна. Разделом нижнего меню каталог не
                // стал — заходят сюда раз в месяц, а место в полосе
                // отняло бы у занятия.
                title: 'Каталог наборов',
                onBack: () => Navigator.of(context).pop(),
              ),
              Expanded(child: _body(context)),
            ],
          ),
        ),
      ),
    );
  }

  Widget _body(BuildContext context) {
    // Отметка, а не кружок на весь экран (правило в app/CLAUDE.md).
    // Витрина обновляется по сети, но установленные наборы лежат на
    // устройстве, и загораживать их кружком незачем.
    if (_loading) {
      return const Padding(
        padding: Gap.screenH,
        child: Align(
          alignment: Alignment.topCenter,
          child: UpdatingLine(updating: true, label: 'Витрина читается'),
        ),
      );
    }

    final failure = _failure;
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.only(bottom: Gap.xxl),
        children: [
          if (failure != null) ...[
            NoteBanner(text: failure, icon: Icons.cloud_off_outlined),
            const SizedBox(height: Gap.lg),
          ],
          for (final pack in _packs)
            PackRow(
              pack: pack,
              busy: _busy == pack.slug,
              done: _done,
              total: _total,
              onDownload: () => _download(pack),
              onRemove: () => _remove(pack),
            ),
          if (_packs.isEmpty && failure == null)
            // Свежая установка без наборов — исправный случай, и поначалу
            // единственный. Потому это пустое состояние, а не отказ:
            // строка посреди белого экрана читается как поломка.
            const EmptyState(
              icon: Icon(Icons.inventory_2_outlined),
              title: 'Наборов пока нет',
              description:
                  'Купленные наборы появятся здесь и лягут на устройство '
                  'целиком — задачи из них решаются без сети',
            ),
        ],
      ),
    );
  }
}

/// Набор в списке.
class PackRow extends StatelessWidget {
  const PackRow({
    super.key,
    required this.pack,
    required this.busy,
    required this.done,
    required this.total,
    required this.onDownload,
    required this.onRemove,
  });

  final ShelfPack pack;
  final bool busy;
  final int done;
  final int total;
  final VoidCallback onDownload;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Padding(
      padding: const EdgeInsets.only(bottom: Gap.md),
      child: Surface(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(pack.title, style: AppType.titleS.copyWith(color: p.ink)),
            const SizedBox(height: Gap.xs),
            Text(
              '${pack.cases} задач · ${rubles(pack.kopecks)}',
              style: AppType.caption.copyWith(color: p.inkFaint),
            ),
            if (pack.summaryMd.isNotEmpty) ...[
              const SizedBox(height: Gap.sm),
              Text(
                pack.summaryMd,
                style: AppType.body.copyWith(color: p.inkMuted),
              ),
            ],
            const SizedBox(height: Gap.md),
            _action(context),
          ],
        ),
      ),
    );
  }

  Widget _action(BuildContext context) {
    final p = context.palette;
    if (busy) {
      // Число, а не одна полоса: набор качается минутами, и врач должен
      // видеть, что дело идёт, а не гадать, не завис ли телефон.
      return Row(
        children: [
          SizedBox(
            width: 120,
            // Тот же градиент, что и на прочих шкалах: «сколько
            // пройдено» читается везде одним движением.
            child: ProgressBar(
              value: total > 0 ? done / total : 0,
              gradient: p.energyGradient,
              shimmer: true,
            ),
          ),
          const SizedBox(width: Gap.md),
          Text('$done из $total', style: AppType.label.copyWith(color: p.ink)),
        ],
      );
    }

    if (!pack.owned) {
      // Цена уже показана строкой выше; здесь — что с этим делать.
      return Text(
        'Набор ещё не открыт. Он появится, как только будет оплачен',
        style: AppType.caption.copyWith(color: p.inkFaint),
      );
    }

    if (pack.isStale) {
      return Row(
        children: [
          FilledButton(onPressed: onDownload, child: const Text('Обновить')),
          const SizedBox(width: Gap.md),
          // Занимает остаток строки и переносится: на узком экране, а тем
          // более при крупном системном шрифте, подпись рядом с кнопкой не
          // умещается — и вместо переноса Flutter рисует полосу отказа.
          Expanded(
            child: Text(
              'на устройстве выпуск ${pack.installed}',
              style: AppType.caption.copyWith(color: p.inkFaint),
            ),
          ),
        ],
      );
    }

    if (pack.isInstalled) {
      return Row(
        children: [
          Icon(Icons.check, size: 18, color: p.success),
          const SizedBox(width: Gap.sm),
          // Обещание точное: без сети работают и лента, и повторение по
          // расписанию — расписание лежит в местной базе. Пока его там не
          // было, здесь стояло «задачи открываются без сети», и это было
          // единственное, что можно было написать не соврав.
          // Expanded вместо Spacer: он и отодвигает «Убрать» к краю, и
          // позволяет подписи перенестись. Со Spacer подпись оставалась
          // неограниченной, и на узком экране строка упиралась в край —
          // вместе с кнопкой, которой врач убирает набор.
          Expanded(
            child: Text(
              'Скачан, работает без сети',
              style: AppType.caption.copyWith(color: p.inkMuted),
            ),
          ),
          TextButton(onPressed: onRemove, child: const Text('Убрать')),
        ],
      );
    }

    return FilledButton(onPressed: onDownload, child: const Text('Скачать'));
  }
}
