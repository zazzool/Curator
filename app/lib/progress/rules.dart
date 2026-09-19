/// Прогресс: опыт, уровни и повторение по интервалам.
///
/// # Это вторая реализация одного расчёта, и это требование
///
/// Врач без сети должен видеть свой опыт, уровень и то, что ему сегодня
/// повторять, а не пустой экран. Значит, считать умеют и сервер, и
/// приложение. Две реализации расходятся МОЛЧА: на устройстве один
/// уровень, в отчёте другой, и кто прав, выяснять будет некому.
///
/// Сходятся они только благодаря общему эталону —
/// `shared/progress-fixtures.json`. Он не набор примеров для удобства, а
/// определение расчёта: где файл и код спорят, прав файл. Отсутствие файла
/// роняет проверку, а не пропускает её.
///
/// Здесь нет ни сети, ни хранилища: расчёт, знающий про них, нельзя
/// проверить эталоном, не подняв их.
library;

/// Правила расчёта, какими их задаёт эталон.
///
/// Не константами по месту: пороги и награды правят, глядя на поведение
/// врачей, и сборка ради порога не выпускается. Приложение читает их с
/// сервера и считает по ним само — по тому же эталону.
class Rules {
  const Rules({
    required this.thresholds,
    required this.correct,
    required this.wrong,
    required this.repeatCorrect,
    required this.repeatWrong,
    required this.reviewCorrect,
    required this.reviewWrong,
    required this.easeStart,
    required this.easeFloor,
    required this.firstInterval,
    required this.secondInterval,
  });

  /// Пороги уровней. Список закрыт сверху: набравший больше последнего
  /// порога остаётся на последнем уровне, потому что бесконечная лестница
  /// обесценивает верх.
  final List<int> thresholds;

  final int correct;
  final int wrong;
  final int repeatCorrect;
  final int repeatWrong;
  final int reviewCorrect;
  final int reviewWrong;

  final double easeStart;
  final double easeFloor;
  final int firstInterval;
  final int secondInterval;

  /// Правила по умолчанию.
  ///
  /// Повторяют эталон, но эталоном не являются: проверка сверяет их с
  /// файлом и отказывает при расхождении. Числа здесь затем, чтобы свежая
  /// установка работала до первого обращения к серверу, а не затем, чтобы
  /// быть вторым местом, где живут пороги.
  static const Rules defaults = Rules(
    thresholds: [0, 100, 250, 500, 900, 1500, 2400, 3600, 5200, 7200],
    correct: 10,
    wrong: 3,
    repeatCorrect: 0,
    repeatWrong: 0,
    reviewCorrect: 6,
    reviewWrong: 2,
    easeStart: 2.5,
    easeFloor: 1.3,
    firstInterval: 1,
    secondInterval: 6,
  );

  /// Уровень при таком опыте.
  ///
  /// Новый врач уже первого уровня, а не нулевого: нулевой читается как
  /// «ты никто», и это первое, что человек видит.
  int level(int xp) {
    var out = 1;
    for (var i = 0; i < thresholds.length; i++) {
      if (xp >= thresholds[i]) out = i + 1;
    }
    return out;
  }

  /// Сколько опыта даёт попытка.
  ///
  /// Повторный разбор той же задачи награды не даёт: иначе опыт набивается
  /// одной задачей, и уровень перестаёт что-либо означать. Повторение по
  /// расписанию — другое дело: это работа, а не набивание.
  int award({
    required bool correct,
    required bool repeat,
    required bool review,
  }) {
    if (review) return correct ? reviewCorrect : reviewWrong;
    if (repeat) return correct ? repeatCorrect : repeatWrong;
    // Не ноль за ошибку. Ноль учит не отвечать, когда не уверен, а это
    // ровно то поведение, от которого задачи и должны отучать.
    return correct ? this.correct : wrong;
  }

  /// Состояние задачи, которую ещё не разбирали.
  ReviewState get fresh => ReviewState(ease: easeStart);

  /// Состояние после ответа.
  ///
  /// SM-2 с оценкой, сведённой к «верно / неверно»: спрашивать у врача,
  /// насколько легко далась задача, значит спрашивать о том, чего он не
  /// знает, и получать шум. Верный ответ идёт как оценка 4, неверный — 2.
  ///
  /// Неверный ответ сбрасывает счёт повторений и ставит интервал в сутки,
  /// но НЕ сбрасывает лёгкость к началу: лёгкость — свойство задачи для
  /// этого врача, накопленное за месяцы, и стирать его из-за одной ошибки
  /// значит терять накопленное на пустом месте.
  ReviewState next(ReviewState before, bool correct, DateTime now) {
    final grade = correct ? 4.0 : 2.0;
    var ease = before.ease == 0 ? easeStart : before.ease;
    ease = _round2(ease + (0.1 - (5 - grade) * (0.08 + (5 - grade) * 0.02)));
    if (ease < easeFloor) {
      // Ниже пола задача возвращается каждый день и превращает повторение
      // в наказание.
      ease = easeFloor;
    }

    int repetitions;
    int interval;
    if (!correct) {
      repetitions = 0;
      interval = firstInterval;
    } else {
      repetitions = before.repetitions + 1;
      switch (repetitions) {
        case 1:
          interval = firstInterval;
        case 2:
          interval = secondInterval;
        default:
          // Умножается на ПРЕЖНЮЮ лёгкость, а не на новую: новая учитывает
          // сегодняшний ответ, а интервал назначается за то, как задача шла
          // до него. Взять новую — значит дважды применить один и тот же
          // ответ и разойтись с эталоном на третьем повторении.
          interval = (before.intervalDays * before.ease).ceil();
      }
    }
    return ReviewState(
      ease: ease,
      intervalDays: interval,
      repetitions: repetitions,
      dueAt: _addDays(now, interval),
    );
  }
}

/// Состояние повторения по одной задаче.
class ReviewState {
  const ReviewState({
    this.ease = 0,
    this.intervalDays = 0,
    this.repetitions = 0,
    this.dueAt,
  });

  final double ease;
  final int intervalDays;
  final int repetitions;
  final DateTime? dueAt;
}

/// Прибавление суток по календарю, а не длительностью.
///
/// `Duration(days: n)` — это ровно n×24 часа, и при переходе на летнее
/// время задача возвращается на час раньше или позже. Врач ждёт её в то же
/// время суток, а не через столько-то часов. Пояс исходного времени
/// сохраняется: подмена местного времени на UTC сдвинула бы срок на разницу
/// поясов — то есть у половины страны на сутки.
DateTime _addDays(DateTime now, int days) => now.isUtc
    ? DateTime.utc(
        now.year,
        now.month,
        now.day + days,
        now.hour,
        now.minute,
        now.second,
        now.millisecond,
        now.microsecond,
      )
    : DateTime(
        now.year,
        now.month,
        now.day + days,
        now.hour,
        now.minute,
        now.second,
        now.millisecond,
        now.microsecond,
      );

/// Округление лёгкости до сотых.
///
/// Не украшение: лёгкость складывается сотнями шагов, и без округления Go и
/// Dart разойдутся на последнем знаке — а сравнивает их эталон точным
/// равенством.
double _round2(double v) => (v * 100).round() / 100;
