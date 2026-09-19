/// Экран разбора: то, ради чего приложение и ставят.
///
/// Показывает задачу, принимает ответ, показывает разбор и кладёт попытку
/// в очередь. На сервер попытка уходит сама, когда есть сеть: ждать её
/// врач не должен — он занимается в метро.
library;

import 'dart:math';

import 'package:flutter/material.dart';

import '../api/client.dart';
import '../packs/store.dart';
import 'feed.dart';
import 'model.dart';
import 'outbox.dart';

/// Откуда берутся задачи.
enum PracticeSource {
  /// Лента: что решать дальше.
  feed,

  /// Расписание повторения. Попытка уезжает с пометкой режима — по ней
  /// сервер начисляет другую награду: повторение это работа, а не
  /// набивание опыта одной задачей.
  review,
}

class PracticeScreen extends StatefulWidget {
  const PracticeScreen({
    super.key,
    required this.api,
    required this.outbox,
    this.packs,
    this.source = PracticeSource.feed,
  });

  final Api api;
  final Outbox outbox;

  /// Наборы на устройстве. Без них лента без сети пуста — но экран
  /// работает и так: повторение без сети всё равно требует местной базы,
  /// которой пока нет.
  final PackStore? packs;

  final PracticeSource source;

  @override
  State<PracticeScreen> createState() => _PracticeScreenState();
}

class _PracticeScreenState extends State<PracticeScreen> {
  final _random = Random();

  List<CaseItem> _cases = [];
  int _at = 0;
  Option? _chosen;
  DateTime _shownAt = DateTime.now();

  bool _loading = true;
  ApiFailure? _failure;
  int _waiting = 0;

  CaseItem? get _current => _at < _cases.length ? _cases[_at] : null;

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
      final feed = Feed(widget.api, widget.packs);
      final cases = widget.source == PracticeSource.review
          ? await feed.due()
          : (await feed.page(limit: 20)).cases;
      if (!mounted) return;
      setState(() {
        _cases = cases;
        _at = 0;
        _chosen = null;
        _shownAt = DateTime.now();
        _loading = false;
      });
    } on ApiFailure catch (failure) {
      if (!mounted) return;
      setState(() {
        _failure = failure;
        _loading = false;
      });
    }
    await _refreshWaiting();
  }

  Future<void> _refreshWaiting() async {
    final left = await widget.outbox.pending();
    if (!mounted) return;
    setState(() => _waiting = left);
  }

  Future<void> _answer(CaseItem one, Option option) async {
    setState(() => _chosen = option);

    // Ключ повторности выдаёт устройство, а не сервер: придуманный
    // сервером ключ не пережил бы обрыва ровно в тот момент, ради которого
    // он заведён. Время с запасом случайности — две попытки в одну
    // микросекунду на одном устройстве невозможны, но ключ, совпавший
    // случайно, потерял бы разбор молча.
    final key =
        '${DateTime.now().microsecondsSinceEpoch}-${_random.nextInt(1 << 32)}';

    await widget.outbox.add(
      PendingAttempt(
        caseId: one.id,
        correct: one.isCorrect(option),
        answer: one.chosenValue(option),
        mode: widget.source == PracticeSource.review ? 'review' : '',
        spentMs: DateTime.now().difference(_shownAt).inMilliseconds,
        idemKey: key,
        happenedAt: DateTime.now(),
      ),
    );

    // Отправка не ждётся и не показывается отказом: очередь сохранит
    // разбор, а врачу здесь важен разбор задачи, а не состояние сети.
    await widget.outbox.flush();
    await _refreshWaiting();
  }

  void _next() {
    setState(() {
      _at++;
      _chosen = null;
      _shownAt = DateTime.now();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(
          widget.source == PracticeSource.review ? 'Повторение' : 'Задачи',
        ),
        bottom: _waiting == 0
            ? null
            // Число ждущих отправки показывается, а не прячется: врач,
            // занимавшийся весь вечер без сети, должен видеть, что его
            // работа не потеряна.
            : PreferredSize(
                preferredSize: const Size.fromHeight(24),
                child: Padding(
                  padding: const EdgeInsets.only(bottom: 6),
                  child: Text('Ждут отправки: $_waiting'),
                ),
              ),
      ),
      body: SafeArea(child: _body(context)),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) return const Center(child: CircularProgressIndicator());

    final failure = _failure;
    if (failure != null) {
      return _Message(text: failure.message, action: 'Ещё раз', onTap: _load);
    }

    final one = _current;
    if (one == null) {
      return _Message(
        text: switch ((widget.source, _cases.isEmpty)) {
          // «Сегодня нечего повторять» — исправный случай и самый частый
          // из всех, и сказать это надо так, чтобы врач не искал поломку.
          (PracticeSource.review, true) => 'Сегодня повторять нечего',
          (PracticeSource.review, false) => 'Повторение на сегодня закончено',
          (_, true) => 'Задач пока нет. Загляните позже',
          (_, false) => 'На сегодня всё. Возвращайтесь завтра',
        },
        action: 'Обновить',
        onTap: _load,
      );
    }
    return CaseView(
      one: one,
      chosen: _chosen,
      onChoose: (option) => _answer(one, option),
      onNext: _next,
    );
  }
}

/// Задача на экране: условие, варианты и разбор после ответа.
///
/// Открыт проверке намеренно: показ задачи — самое, что здесь есть, и
/// проверять его через сеть значит проверять сеть.
class CaseView extends StatelessWidget {
  const CaseView({
    super.key,
    required this.one,
    required this.chosen,
    required this.onChoose,
    required this.onNext,
  });

  final CaseItem one;
  final Option? chosen;
  final ValueChanged<Option> onChoose;
  final VoidCallback onNext;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final answered = chosen != null;

    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        if (one.unitLabel.isNotEmpty)
          Text(one.unitLabel, style: theme.textTheme.labelMedium),
        if (one.title.isNotEmpty) ...[
          const SizedBox(height: 4),
          Text(one.title, style: theme.textTheme.titleMedium),
        ],
        const SizedBox(height: 12),

        // Условие идёт кусками, а не одним текстом: кусок — это то, что
        // подтверждает положение источника, и в разборе он подсвечивается
        // отдельно.
        for (final segment in one.segments) ...[
          Text(segment.text, style: theme.textTheme.bodyLarge),
          if (answered && segment.statements.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Text(
                segment.statements.join(', '),
                style: theme.textTheme.labelSmall,
              ),
            ),
          const SizedBox(height: 8),
        ],

        const SizedBox(height: 8),
        for (final option in one.options)
          _OptionTile(
            option: option,
            state: _stateOf(option),
            onTap: answered ? null : () => onChoose(option),
          ),

        if (answered) ...[
          const SizedBox(height: 16),
          Text(
            one.isCorrect(chosen!) ? 'Верно' : 'Неверно',
            style: theme.textTheme.titleMedium,
          ),
          if (one.explanationMd.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(one.explanationMd, style: theme.textTheme.bodyMedium),
          ],
          const SizedBox(height: 16),
          FilledButton(onPressed: onNext, child: const Text('Дальше')),
        ],
      ],
    );
  }

  _OptionState _stateOf(Option option) {
    if (chosen == null) return _OptionState.plain;
    // Верный вариант показывается всегда, а не только когда он выбран:
    // врач, ответивший неверно, должен увидеть, как было правильно, здесь
    // же — иначе он уйдёт с экрана, так и не узнав.
    if (one.chosenValue(option) == one.answer) return _OptionState.right;
    if (identical(option, chosen)) return _OptionState.wrong;
    return _OptionState.plain;
  }
}

enum _OptionState { plain, right, wrong }

class _OptionTile extends StatelessWidget {
  const _OptionTile({required this.option, required this.state, this.onTap});

  final Option option;
  final _OptionState state;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final color = switch (state) {
      _OptionState.plain => scheme.outlineVariant,
      _OptionState.right => scheme.primary,
      _OptionState.wrong => scheme.error,
    };

    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: InkWell(
        onTap: onTap,
        child: Container(
          width: double.infinity,
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            // Волосяная граница, а не тень: тень допустима только у того,
            // что физически висит над страницей.
            border: Border.all(color: color),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Row(
            children: [
              if (option.label.isNotEmpty) ...[
                Text(option.label),
                const SizedBox(width: 12),
              ],
              Expanded(child: Text(option.text)),
            ],
          ),
        ),
      ),
    );
  }
}

class _Message extends StatelessWidget {
  const _Message({
    required this.text,
    required this.action,
    required this.onTap,
  });

  final String text;
  final String action;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(text, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            OutlinedButton(onPressed: onTap, child: Text(action)),
          ],
        ),
      ),
    );
  }
}
