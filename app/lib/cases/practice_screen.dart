/// Экран разбора: то, ради чего приложение и ставят.
///
/// Показывает задачу, принимает ответ, показывает разбор и кладёт попытку
/// в очередь. На сервер попытка уходит сама, когда есть сеть: ждать её
/// врач не должен — он занимается в метро.
library;

import 'dart:math';

import 'package:flutter/material.dart';

import '../account/recover_button.dart';
import '../api/client.dart';
import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/difficulty_dots.dart';
import '../core/ui/motion.dart';
import '../core/ui/quiet_progress.dart';
import '../core/ui/surface.dart';
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
    this.feed,
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

  /// Лента. Своя собирается из двери, наборов и расписания — и собирается
  /// в `_load`, а не здесь, потому что экран переживает смену записи.
  ///
  /// Подменяется проверкой ровно по правилу из `app/CLAUDE.md`: проверка
  /// виджетов до сокета не достучится (Flutter подменяет `HttpClient` и
  /// отвечает 400 на всё), и без этого шва главное действие приложения —
  /// «ответ врача лёг в очередь» — не проверялось ничем.
  final Feed? feed;

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

  /// Разбор, который не удалось записать на устройство.
  ///
  /// Хранится целиком, а не признаком: повторная запись обязана уйти с
  /// ТЕМ ЖЕ ключом повторности. Выдай мы новый, и разбор, записавшийся с
  /// первого раза наполовину, лёг бы вторым — а дважды положенная попытка
  /// портит и прогресс, и решаемость, причём незаметно: числа остаются
  /// правдоподобными.
  PendingAttempt? _unsaved;

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
      final feed =
          widget.feed ?? Feed(widget.api, widget.packs, widget.schedule);
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
    int left;
    try {
      left = await widget.outbox.pending();
    } catch (error) {
      // Счёт ждущих отправки читается из той же очереди, и отказ чтения
      // не должен ронять экран, на котором врач занимается: число просто
      // остаётся прежним.
      debugPrint('очередь не сосчиталась: $error');
      return;
    }
    if (!mounted) return;
    setState(() => _waiting = left);
  }

  Future<void> _answer(CaseItem one, Option option) async {
    setState(() => _chosen = option);

    // Отклик в руку раньше, чем глаз дочитает разбор: врач занимается на
    // ходу, и в эту секунду телефон он чувствует, а не разглядывает.
    // Отказ платформы тут проглатывается внутри Haptics — на настольной
    // машине и в проверках канала вибрации нет вовсе.
    if (one.isCorrect(option)) {
      Haptics.success();
    } else {
      Haptics.failure();
    }

    // Ключ повторности выдаёт устройство, а не сервер: придуманный
    // сервером ключ не пережил бы обрыва ровно в тот момент, ради которого
    // он заведён. Время с запасом случайности — две попытки в одну
    // микросекунду на одном устройстве невозможны, но ключ, совпавший
    // случайно, потерял бы разбор молча.
    final key =
        '${DateTime.now().microsecondsSinceEpoch}-${_random.nextInt(1 << 32)}';

    await _keep(
      PendingAttempt(
        caseId: one.id,
        correct: one.isCorrect(option),
        answer: one.chosenValue(option),
        mode: widget.source == PracticeSource.review ? 'review' : '',
        spentMs: PendingAttempt.clampSpent(
          DateTime.now().difference(_shownAt).inMilliseconds,
        ),
        idemKey: key,
        happenedAt: DateTime.now(),
      ),
    );
  }

  /// Кладёт разбор в очередь и доводит дело до конца.
  ///
  /// Отдельной работой, а не хвостом `_answer`, потому что зовётся ещё и
  /// повторно — с той же попыткой, когда первая запись не прошла. Задача
  /// при этом берётся из самой попытки: к моменту повтора врач может быть
  /// уже на следующей, а записать надо ту, которую он разобрал.
  Future<void> _keep(PendingAttempt attempt) async {
    // Запись на устройство отказывает: на телефоне кончается место.
    // Прежде отказ уходил отсюда необработанным, и это было самым дорогим
    // местом во всём приложении: РАЗБОР ВРАЧА ИСЧЕЗАЛ, сообщения не было
    // никакого, кнопка «Дальше» появлялась как обычно, и врач продолжал
    // заниматься впустую — узнал бы он об этом по несошедшемуся уровню
    // через неделю.
    try {
      await widget.outbox.add(attempt);
      if (mounted) setState(() => _unsaved = null);
    } catch (error) {
      debugPrint('разбор не записался в очередь: $error');
      if (!mounted) return;
      setState(() => _unsaved = attempt);
      return;
    }

    // Срок следующего повторения считается здесь же и ложится на
    // устройство. Иначе повторение без сети показывало бы вчерашний
    // список: разбор, сделанный в метро, сервер увидит только назавтра,
    // а расписание нужно врачу сегодня.
    //
    // Расчёт тот же самый, общий с сервером (`progress/rules.dart`), и
    // второй его реализации здесь нет: сойтись они могут только так.
    final schedule = widget.schedule;
    var scheduled = false;
    if (schedule != null) {
      try {
        final rules = Rules.defaults;
        final before = await schedule.state(attempt.caseId) ?? rules.fresh;
        await schedule.put(
          attempt.caseId,
          rules.next(before, attempt.correct, DateTime.now()),
        );
        scheduled = true;
      } catch (error) {
        // Молча для врача, но не молча для нас: расписание на устройстве
        // — местная копия, хозяин ей сервер, и разбор уже лежит в
        // очереди. Потеряно здесь только «когда повторить без сети», и
        // это вернётся первым же ответом сервера.
        debugPrint('срок повторения не записался: $error');
      }
    }

    // Отправка не ждётся и не показывается отказом: очередь сохранит
    // разбор, а врачу здесь важен разбор задачи, а не состояние сети.
    Flushed? sent;
    try {
      sent = await widget.outbox.flush();
    } catch (error) {
      // Отправка пишет очередь заново, и запись отказывает по тем же
      // причинам. Разбор при этом уже лежит: отказ здесь значит «уедет
      // позже», а не «пропал».
      debugPrint('очередь не отправилась: $error');
    }

    // Доехавшее перестаёт быть «известным только нам»: с этого мгновения
    // слово сервера о сроке старше нашего.
    if (schedule != null && scheduled && sent != null) {
      if (sent.sent > 0 && sent.failure == null) {
        try {
          await schedule.markSynced([attempt.caseId]);
        } catch (error) {
          debugPrint('отметка об отправке не записалась: $error');
        }
      }
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
    // Шапка стоит в потоке содержимого, а не полосой `AppBar` сверху:
    // она уезжает вместе с условием при прокрутке и не отнимает полосу у
    // того, ради чего экран открыт. Заголовок при этом остаётся крупным —
    // с него начинается чтение.
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: Gap.screenH,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ScreenHeader(
                // Заголовок называет режим тем же словом, что и плитка,
                // с которой врач сюда вошёл: назовись экран иначе, он
                // читался бы как «я попал не туда».
                title: widget.source == PracticeSource.review
                    ? 'Повторение'
                    : 'Новая задача',
                // Число ждущих отправки показывается, а не прячется: врач,
                // занимавшийся весь вечер без сети, должен видеть, что его
                // работа не потеряна.
                subtitle: _waiting == 0 ? null : 'Ждут отправки: $_waiting',
                // Экран открыт поверх «Практики»: возврат обязан вернуть
                // врача к выбору режима.
                onBack: () => Navigator.of(context).pop(),
              ),
              // Пока задачи читаются, экран не утверждает ничего: кружок
              // на весь экран объявлял бы работу невозможной, пока ответит
              // сервер, — а в метро всё нужное лежит на устройстве.
              UpdatingLine(updating: _loading, label: 'Задачи обновляются'),
              Expanded(child: _body(context)),
            ],
          ),
        ),
      ),
    );
  }

  Widget _body(BuildContext context) {
    final unsaved = _unsaved;
    if (unsaved == null) return _content(context);

    // Полоса стоит НАД задачей и не уходит с переходом к следующей: пока
    // разбор не записан, он не уйдёт на сервер, и сказать об этом надо
    // там, где врач смотрит, а не в настройках. Повтор — рядом: место на
    // устройстве освобождают в ту же минуту, и второй попытке надо дать
    // случиться, не отнимая у врача занятия.
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const NoteBanner(
          text:
              'Разбор не записался на устройство — похоже, на нём нет '
              'места. Пока он не записан, на сервер он не уйдёт.',
          icon: Icons.sd_card_alert_outlined,
        ),
        const SizedBox(height: Gap.sm),
        Align(
          alignment: Alignment.centerRight,
          child: OutlinedButton(
            onPressed: () => _keep(unsaved),
            child: const Text('Записать ещё раз'),
          ),
        ),
        const SizedBox(height: Gap.md),
        Expanded(child: _content(context)),
      ],
    );
  }

  Widget _content(BuildContext context) {
    if (_loading) return const SizedBox.shrink();

    final failure = _failure;
    if (failure != null) {
      // Отозванный токен лечится не повтором, а возвратом записи по
      // почте, и предложить это надо здесь: экран разбора — то место,
      // где врач в это упирается, а возврат лежит в трёх нажатиях вглубь
      // настроек, и ничто на него не указывает.
      if (failure.lostDevice) {
        return EmptyState(
          icon: const Icon(Icons.no_accounts_outlined),
          title: 'Устройство не опознано',
          description: failure.message,
          action: RecoverAccessButton(api: widget.api, onRecovered: _load),
        );
      }
      return _Message(
        icon: Icons.cloud_off_outlined,
        title: 'Задачи не загрузились',
        text: failure.message,
        action: 'Ещё раз',
        onTap: _load,
      );
    }

    final one = _current;
    if (one == null) {
      return _Message(
        icon: widget.source == PracticeSource.review
            ? Icons.replay_outlined
            : Icons.school_outlined,
        title: switch ((widget.source, _cases.isEmpty)) {
          // «Сегодня нечего повторять» — исправный случай и самый частый
          // из всех, и сказать это надо так, чтобы врач не искал поломку.
          (PracticeSource.review, true) => 'Сегодня повторять нечего',
          (PracticeSource.review, false) => 'Повторение на сегодня закончено',
          (_, true) => 'Задач пока нет',
          (_, false) => 'На сегодня всё',
        },
        text: switch ((widget.source, _cases.isEmpty)) {
          (PracticeSource.review, _) =>
            'Задачи вернутся к повторению по '
                'своим срокам — заходить раньше незачем.',
          (_, true) => 'Загляните позже: новые задачи приходят с сервера.',
          (_, false) => 'Возвращайтесь завтра.',
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
    final p = context.palette;
    final answered = chosen != null;
    final right = answered && one.isCorrect(chosen!);

    return ListView(
      padding: const EdgeInsets.only(bottom: Gap.xxl),
      children: [
        _Marks(one: one, showMarkup: answered),
        if (one.title.isNotEmpty) ...[
          const SizedBox(height: Gap.md),
          Prose(one.title, style: AppType.titleL.copyWith(color: p.ink)),
        ],
        const SizedBox(height: Gap.lg),

        // Условие идёт кусками, а не одним текстом: кусок — это то, что
        // подтверждает положение источника, и в разборе он подсвечивается
        // отдельно.
        for (final segment in one.segments) ...[
          // Увеличенный интерлиньяж: условие читают подолгу, и кегль
          // разговорных подписей на длинном тексте утомляет.
          Prose(segment.text, style: AppType.reading.copyWith(color: p.ink)),
          if (answered && segment.statements.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: Gap.sm),
              child: Wrap(
                spacing: Gap.sm,
                runSpacing: Gap.sm,
                children: [
                  for (final mark in segment.statements) _Mark(text: mark),
                ],
              ),
            ),
          const SizedBox(height: Gap.md),
        ],

        const SizedBox(height: Gap.sm),
        for (final option in one.options)
          _OptionTile(
            option: option,
            state: _stateOf(option),
            onTap: answered ? null : () => onChoose(option),
          ),

        if (answered) ...[
          const SizedBox(height: Gap.xl),
          // Исход назван и словом, и знаком: цвет различают не все, и
          // «верно» одним оттенком рамки у части врачей не читается
          // вовсе. Зелёный здесь не берётся — успех у нас оливковый, а
          // красный занят строго неверным ответом.
          Row(
            children: [
              Icon(
                right ? Icons.check_circle : Icons.cancel_outlined,
                size: 22,
                color: right ? p.success : p.danger,
              ),
              const SizedBox(width: Gap.sm),
              Text(
                right ? 'Верно' : 'Неверно',
                style: AppType.titleM.copyWith(
                  color: right ? p.success : p.danger,
                ),
              ),
            ],
          ),
          if (one.explanationMd.isNotEmpty) ...[
            const SizedBox(height: Gap.md),
            Prose(
              one.explanationMd,
              style: AppType.reading.copyWith(color: p.inkMuted),
            ),
          ],
          const SizedBox(height: Gap.xl),
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
    final p = context.palette;
    final color = switch (state) {
      _OptionState.plain => p.hairline,
      // Успех оливковый, а не зелёный «из палитры системы»: зелёный в
      // этой гамме выглядит вставленным из чужого приложения. Красный
      // занят строго неверным ответом и больше нигде не встречается.
      _OptionState.right => p.success,
      _OptionState.wrong => p.danger,
    };
    // Знак исхода вместо одного цвета рамки: цвет различают не все, и
    // задача, разобранная по цвету, у части врачей не разбирается вовсе.
    final mark = switch (state) {
      _OptionState.plain => null,
      _OptionState.right => Icons.check,
      _OptionState.wrong => Icons.close,
    };
    // Исход словами, а не одной картинкой.
    //
    // Довод про цвет выше был написан и наполовину не исполнен: голая
    // `Icon` в дерево доступности не попадает вовсе, и незрячему врачу не
    // говорилось ни «верно», ни «неверно» — вариант читался так же, как
    // до ответа. То есть у того, кто не видит рамки, задача не
    // разбиралась в принципе.
    final outcome = switch (state) {
      _OptionState.plain => null,
      _OptionState.right => 'верно',
      _OptionState.wrong => 'неверно',
    };

    // Сторона кружка с меткой растёт вместе с системным шрифтом.
    //
    // clamp возвращает num, а не double, и без toDouble здесь была бы не
    // раскладка, а отказ разбора: width ждёт double.
    final markScale = MediaQuery.textScalerOf(
      context,
    ).scale(1.0).clamp(1.0, 2.0).toDouble();
    final markSide = 26 * markScale;

    return Padding(
      padding: const EdgeInsets.only(bottom: Gap.md),
      child: Semantics(
        // Исход стоит ПЕРВЫМ: диктор читает подпись подряд, а врач ждёт
        // ответа на «угадал или нет», а не пересказа варианта, который он
        // только что выбрал сам.
        label: outcome == null ? null : '$outcome.',
        // Кнопкой вариант объявляется, только пока по нему можно
        // ответить: после ответа это уже не кнопка, а результат, и
        // «кнопка, недоступна» отправило бы врача её искать.
        button: onTap != null,
        child: InkWell(
          onTap: onTap,
          borderRadius: Radii.surfaceAll,
          splashColor: p.accent.withValues(alpha: 0.06),
          highlightColor: p.accent.withValues(alpha: 0.04),
          child: Container(
            width: double.infinity,
            padding: const EdgeInsets.symmetric(
              horizontal: Gap.lg,
              vertical: Gap.md,
            ),
            decoration: BoxDecoration(
              color: p.surface,
              // Волосяная граница, а не тень: тень допустима только у того,
              // что физически висит над страницей.
              border: Border.all(
                color: color,
                width: state == _OptionState.plain ? 1 : 1.5,
              ),
              borderRadius: Radii.surfaceAll,
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (option.label.isNotEmpty) ...[
                  // Метка в кружке постоянного размера: у меток «А» и «VIII»
                  // разная ширина, и без кружка текст вариантов начинается
                  // с разных мест.
                  //
                  // Постоянен он в ЛОГИЧЕСКИХ точках, а не в пикселях:
                  // кружок растёт вместе с системным шрифтом. Жёстких
                  // 26×26 хватало метке «А» и не хватало «VIII» уже при
                  // полуторном шрифте — метка обрезалась, и врач с
                  // крупным шрифтом видел вариант без номера. Потолок
                  // масштаба — вдвое: дальше кружок занимает половину
                  // строки, и текст варианта теряет больше, чем метка
                  // выигрывает.
                  Container(
                    width: markSide,
                    height: markSide,
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      border: Border.all(color: color),
                    ),
                    child: Text(
                      option.label,
                      style: AppType.label.copyWith(color: color),
                    ),
                  ),
                  const SizedBox(width: Gap.md),
                ],
                Expanded(
                  child: Prose(
                    option.text,
                    style: AppType.body.copyWith(color: p.ink),
                  ),
                ),
                if (mark != null) ...[
                  const SizedBox(width: Gap.md),
                  // Картинка прячется от диктора намеренно: исход уже сказан
                  // подписью выше, и второй раз он прозвучал бы как ещё
                  // один вариант ответа.
                  ExcludeSemantics(child: Icon(mark, size: 18, color: color)),
                ],
              ],
            ),
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
      spacing: Gap.sm,
      runSpacing: Gap.sm,
      children: [
        Pill(label: one.kind.word, icon: const Icon(Icons.category_outlined)),
        if (one.unitLabel.isNotEmpty)
          Pill(label: one.unitLabel, icon: const Icon(Icons.label_outline)),
        if (one.difficulty > 0)
          // Трудность точками, а не числом: «3» само по себе не говорит
          // ничего — из чего эти три, видно только по шкале целиком.
          Pill(
            label: 'трудность',
            icon: DifficultyDots(level: one.difficulty),
          ),
        if (showMarkup && marked > 0)
          Pill(
            label: 'Подтверждают: $marked из ${one.segments.length}',
            icon: const Icon(Icons.link),
          ),
      ],
    );
  }
}

/// Обозначение положения, которое подтверждает кусок условия.
class _Mark extends StatelessWidget {
  const _Mark({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    // Плотная таблетка на подложке акцента: обозначений под куском
    // условия бывает несколько подряд, и рамка у каждой рябила бы.
    return Pill(
      label: text,
      icon: const Icon(Icons.link),
      color: p.accent,
      background: p.accentSoft,
      dense: true,
    );
  }
}

/// Экран без задачи: отказ или «на сегодня всё».
///
/// Выход из положения обязателен даже там, где ничего не сломалось:
/// пустой экран без кнопки читается как тупик, и врач уходит из
/// приложения вместо того, чтобы обновить список.
class _Message extends StatelessWidget {
  const _Message({
    required this.icon,
    required this.title,
    required this.text,
    required this.action,
    required this.onTap,
  });

  final IconData icon;
  final String title;
  final String text;
  final String action;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => EmptyState(
    icon: Icon(icon),
    title: title,
    description: text,
    action: OutlinedButton(onPressed: onTap, child: Text(action)),
  );
}
