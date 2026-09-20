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
      appBar: AppBar(title: const Text('Наборы')),
      body: SafeArea(child: _body(context)),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) return const Center(child: CircularProgressIndicator());

    final failure = _failure;
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (failure != null) ...[
            Text(failure, textAlign: TextAlign.center),
            const SizedBox(height: 16),
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
            // единственный.
            const Text('Наборов пока нет', textAlign: TextAlign.center),
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
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        // Волосяная граница, а не тень.
        border: Border.all(color: theme.colorScheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(pack.title, style: theme.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            '${pack.cases} задач · ${rubles(pack.kopecks)}',
            style: theme.textTheme.bodySmall,
          ),
          if (pack.summaryMd.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(pack.summaryMd, style: theme.textTheme.bodyMedium),
          ],
          const SizedBox(height: 12),
          _action(context),
        ],
      ),
    );
  }

  Widget _action(BuildContext context) {
    if (busy) {
      // Число, а не одна полоса: набор качается минутами, и врач должен
      // видеть, что дело идёт, а не гадать, не завис ли телефон.
      return Row(
        children: [
          SizedBox(
            width: 120,
            child: LinearProgressIndicator(
              value: total > 0 ? done / total : null,
            ),
          ),
          const SizedBox(width: 12),
          Text('$done из $total'),
        ],
      );
    }

    if (!pack.owned) {
      // Цена уже показана строкой выше; здесь — что с этим делать.
      return Text(
        'Набор ещё не открыт. Он появится, как только будет оплачен',
        style: Theme.of(context).textTheme.bodySmall,
      );
    }

    if (pack.isStale) {
      return Row(
        children: [
          FilledButton(onPressed: onDownload, child: const Text('Обновить')),
          const SizedBox(width: 12),
          // Занимает остаток строки и переносится: на узком экране, а тем
          // более при крупном системном шрифте, подпись рядом с кнопкой не
          // умещается — и вместо переноса Flutter рисует полосу отказа.
          Expanded(child: Text('на устройстве выпуск ${pack.installed}')),
        ],
      );
    }

    if (pack.isInstalled) {
      return Row(
        children: [
          const Icon(Icons.check, size: 18),
          const SizedBox(width: 8),
          // Обещание точное: без сети работают и лента, и повторение по
          // расписанию — расписание лежит в местной базе. Пока его там не
          // было, здесь стояло «задачи открываются без сети», и это было
          // единственное, что можно было написать не соврав.
          // Expanded вместо Spacer: он и отодвигает «Убрать» к краю, и
          // позволяет подписи перенестись. Со Spacer подпись оставалась
          // неограниченной, и на узком экране строка упиралась в край —
          // вместе с кнопкой, которой врач убирает набор.
          const Expanded(child: Text('Скачан, работает без сети')),
          TextButton(onPressed: onRemove, child: const Text('Убрать')),
        ],
      );
    }

    return FilledButton(onPressed: onDownload, child: const Text('Скачать'));
  }
}
