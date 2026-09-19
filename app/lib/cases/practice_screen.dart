/// Экран разбора: то, ради чего приложение и ставят.
///
/// Показывает задачу, принимает ответ, показывает разбор и кладёт попытку
/// в очередь. На сервер попытка уходит сама, когда есть сеть: ждать её
/// врач не должен — он занимается в метро.
library;

import 'dart:math';

import 'package:flutter/material.dart';

import '../api/client.dart';
import '../db/schedule.dart';
import '../packs/store.dart';
import '../progress/rules.dart';
import '../text/prose.dart';
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
    this.schedule,
    this.source = PracticeSource.feed,
  });

  final Api api;
  final Outbox outbox;

  /// Наборы на устройстве. Без них лента без сети пуста — но экран
  /// работает и так: повторение без сети всё равно требует местной базы,
  /// которой пока нет.
  final PackStore? packs;

  /// Расписание повторения на устройстве. Пока его нет, повторение без
  /// сети невозможно: сроки живут на сервере.
  final Schedule? schedule;

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
      final feed = Feed(widget.api, widget.packs, widget.schedule);
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

    // Срок следующего повторения считается здесь же и ложится на
    // устройство. Иначе повторение без сети показывало бы вчерашний
    // список: разбор, сделанный в метро, сервер увидит только назавтра,
    // а расписание нужно врачу сегодня.
    //
    // Расчёт тот же самый, общий с сервером (`progress/rules.dart`), и
    // второй его реализации здесь нет: сойтись они могут только так.
    final schedule = widget.schedule;
    if (schedule != null) {
      final rules = Rules.defaults;
      final before = await schedule.state(one.id) ?? rules.fresh;
      await schedule.put(
        one.id,
        rules.next(before, one.isCorrect(option), DateTime.now()),
      );
    }

    // Отправка не ждётся и не показывается отказом: очередь сохранит
    // разбор, а врачу здесь важен разбор задачи, а не состояние сети.
    final sent = await widget.outbox.flush();
    // Доехавшее перестаёт быть «известным только нам»: с этого мгновения
    // слово сервера о сроке старше нашего.
    if (schedule != null && sent.sent > 0 && sent.failure == null) {
      await schedule.markSynced([one.id]);
    }
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
///
/// # Условие показывается так же, как критерии справочника
///
/// Тем же `Prose` и с теми же переносами. Врач читает то и другое подряд —
/// сверяет условие с критериями, — и два разных набора кеглей и отступов
/// на соседних экранах читаются как два разных приложения.
///
/// # Разметка показывается после ответа, а не до
///
/// Обозначения положений, которые подтверждает кусок условия, — это и
/// есть подсказка: показанные до ответа, они превращают задачу в чтение
/// с ответом на полях.
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
    final right = answered && one.isCorrect(chosen!);

    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 28),
      children: [
        _Marks(one: one, showMarkup: answered),
        if (one.title.isNotEmpty) ...[
          const SizedBox(height: 10),
          Prose(one.title, style: theme.textTheme.titleLarge),
        ],
        const SizedBox(height: 16),

        // Условие идёт кусками, а не одним текстом: кусок — это то, что
        // подтверждает положение источника, и в разборе он подсвечивается
        // отдельно.
        for (final segment in one.segments) ...[
          Prose(segment.text, style: theme.textTheme.bodyLarge),
          if (answered && segment.statements.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  for (final mark in segment.statements) _Mark(text: mark),
                ],
              ),
            ),
          const SizedBox(height: 12),
        ],

        const SizedBox(height: 6),
        for (final option in one.options)
          _OptionTile(
            option: option,
            state: _stateOf(option),
            onTap: answered ? null : () => onChoose(option),
          ),

        if (answered) ...[
          const SizedBox(height: 20),
          Row(
            children: [
              Icon(
                right ? Icons.check_circle : Icons.cancel_outlined,
                size: 22,
                color: right
                    ? theme.colorScheme.primary
                    : theme.colorScheme.error,
              ),
              const SizedBox(width: 8),
              Text(
                right ? 'Верно' : 'Неверно',
                style: theme.textTheme.titleMedium?.copyWith(
                  color: right
                      ? theme.colorScheme.primary
                      : theme.colorScheme.error,
                ),
              ),
            ],
          ),
          if (one.explanationMd.isNotEmpty) ...[
            const SizedBox(height: 12),
            Prose(one.explanationMd, style: theme.textTheme.bodyMedium),
          ],
          const SizedBox(height: 20),
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
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final color = switch (state) {
      _OptionState.plain => scheme.outlineVariant,
      _OptionState.right => scheme.primary,
      _OptionState.wrong => scheme.error,
    };
    // Знак исхода вместо одного цвета рамки: цвет различают не все, и
    // задача, разобранная по цвету, у части врачей не разбирается вовсе.
    final mark = switch (state) {
      _OptionState.plain => null,
      _OptionState.right => Icons.check,
      _OptionState.wrong => Icons.close,
    };

    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(10),
        child: Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
          decoration: BoxDecoration(
            // Волосяная граница, а не тень: тень допустима только у того,
            // что физически висит над страницей.
            border: Border.all(
              color: color,
              width: state == _OptionState.plain ? 1 : 1.5,
            ),
            borderRadius: BorderRadius.circular(10),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (option.label.isNotEmpty) ...[
                // Метка в кружке постоянного размера: у меток «А» и «VIII»
                // разная ширина, и без кружка текст вариантов начинается
                // с разных мест.
                Container(
                  width: 26,
                  height: 26,
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    border: Border.all(color: color),
                  ),
                  child: Text(
                    option.label,
                    style: theme.textTheme.labelMedium?.copyWith(color: color),
                  ),
                ),
                const SizedBox(width: 12),
              ],
              Expanded(
                child: Prose(option.text, style: theme.textTheme.bodyLarge),
              ),
              if (mark != null) ...[
                const SizedBox(width: 10),
                Icon(mark, size: 18, color: color),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// Полоска над задачей: вид, рубрика, трудность, объём разметки.
///
/// Отвечает на то, что врач оценивает взглядом до чтения: откуда задача и
/// насколько она тяжёлая. Объём разметки показывается только после
/// ответа — до ответа он говорит, сколько в условии значащих кусков, а это
/// подсказка.
class _Marks extends StatelessWidget {
  const _Marks({required this.one, required this.showMarkup});

  final CaseItem one;
  final bool showMarkup;

  @override
  Widget build(BuildContext context) {
    final marked = one.segments.where((s) => s.statements.isNotEmpty).length;

    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        _Fact(icon: Icons.category_outlined, text: one.kind.word),
        if (one.unitLabel.isNotEmpty)
          _Fact(icon: Icons.label_outline, text: one.unitLabel),
        if (one.difficulty > 0)
          _Fact(
            icon: Icons.speed_outlined,
            text: 'Трудность: ${one.difficulty}',
          ),
        if (showMarkup && marked > 0)
          _Fact(
            icon: Icons.link,
            text: 'Подтверждают: $marked из ${one.segments.length}',
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
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      decoration: BoxDecoration(
        border: Border.all(color: theme.colorScheme.outlineVariant),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 14, color: theme.colorScheme.onSurfaceVariant),
          const SizedBox(width: 6),
          Text(text, style: theme.textTheme.labelMedium),
        ],
      ),
    );
  }
}

/// Обозначение положения, которое подтверждает кусок условия.
class _Mark extends StatelessWidget {
  const _Mark({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        border: Border.all(
          color: theme.colorScheme.primary.withValues(alpha: 0.4),
        ),
        borderRadius: BorderRadius.circular(6),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.link, size: 12, color: theme.colorScheme.primary),
          const SizedBox(width: 5),
          Text(
            text,
            style: theme.textTheme.labelSmall?.copyWith(
              color: theme.colorScheme.primary,
            ),
          ),
        ],
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
