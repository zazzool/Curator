/// Каталог знаков отличия глазами приложения.
///
/// # Знак выдаёт сервер, а не устройство
///
/// Номер, дата выдачи, тираж и доля обладателей — сведения о том, каким
/// врач пришёл СРЕДИ ВСЕХ, и вывести их из его собственной истории нельзя.
/// Приложение считает по этому каталогу ровно одно — долю пути к знаку,
/// чтобы показать её без сети.
///
/// Зеркален с `shared/signs-catalog.json`, а через него — с сервером. Два
/// каталога, набранных порознь, расходятся молча, и первым это увидит врач,
/// у которого на экране знак называется иначе, чем в свидетельстве.
library;

import '../progress/metrics.dart';

/// Вид знака. Словарь закрыт: вид, заведённый по месту, приложение не
/// нарисует, а сервер выдаст — и знак повиснет невидимым.
enum SignKind {
  /// За величину, тираж не ограничен.
  metric,

  /// За величину, но тиражом: заслужить можно и не получить.
  edition,

  /// За уже выданные знаки; смотрит на КОГДА-ЛИБО выданное.
  collective,

  /// Переходящий: держит его один, лучший по величине.
  rotating;

  static SignKind? parse(String raw) {
    for (final one in SignKind.values) {
      if (one.name == raw) return one;
    }
    return null;
  }
}

/// Знак, как он объявлен в каталоге.
class Sign {
  const Sign({
    required this.slug,
    required this.title,
    required this.kind,
    this.metric = '',
    this.threshold = 0,
    this.requires = const [],
    this.editionSize = 0,
    this.xp = 0,
  });

  final String slug;
  final String title;
  final SignKind kind;

  /// За какую величину и с какого значения. У собирательного знака их нет.
  final String metric;
  final int threshold;

  /// Знаки, которые должны быть когда-либо выданы. Только у собирательного.
  final List<String> requires;

  /// Тираж. Ноль значит «не ограничен».
  final int editionSize;

  /// Опыт за ВЫДАННЫЙ знак. Знак с тиражом можно заслужить и не получить,
  /// и показывать врачу опыт за знак, которого у него нет, нечем.
  final int xp;
}

/// Каталог целиком, в порядке эталона.
///
/// Порядок значим: знаки показываются списком, и порядок в нём — часть
/// того, что человек видит.
abstract final class Signs {
  static const catalog = <Sign>[
    Sign(
      slug: 'first-steps',
      title: 'Первые шаги',
      kind: SignKind.metric,
      metric: 'casesSolved',
      threshold: 10,
      xp: 50,
    ),
    Sign(
      slug: 'hundred',
      title: 'Сотня',
      kind: SignKind.metric,
      metric: 'casesSolved',
      threshold: 100,
      xp: 200,
    ),
    Sign(
      slug: 'streak-25',
      title: 'Двадцать пять подряд',
      kind: SignKind.metric,
      metric: 'correctStreak',
      threshold: 25,
      xp: 150,
    ),
    Sign(
      slug: 'month',
      title: 'Месяц занятий',
      kind: SignKind.metric,
      metric: 'daysActive',
      threshold: 30,
      xp: 200,
    ),
    Sign(
      slug: 'reviewer',
      title: 'Возвращается',
      kind: SignKind.metric,
      metric: 'reviewsDone',
      threshold: 100,
      xp: 150,
    ),
    Sign(
      slug: 'breadth',
      title: 'Вширь',
      kind: SignKind.metric,
      metric: 'sourcesTouched',
      threshold: 5,
      xp: 100,
    ),
    Sign(
      slug: 'pioneer',
      title: 'Первопроходец',
      kind: SignKind.edition,
      metric: 'casesSolved',
      threshold: 1,
      editionSize: 100,
      xp: 300,
    ),
    Sign(
      slug: 'order-of-accuracy',
      title: 'Орден Точности',
      kind: SignKind.collective,
      requires: ['streak-25', 'hundred'],
      xp: 400,
    ),
    Sign(
      slug: 'primus',
      title: 'Primus inter pares',
      kind: SignKind.rotating,
      metric: 'correctStreak',
      threshold: 25,
      xp: 100,
    ),
  ];

  static Sign? known(String slug) {
    for (final one in catalog) {
      if (one.slug == slug) return one;
    }
    return null;
  }

  /// Доля пути к знаку: от 0 до 1.
  ///
  /// Единственное, что приложение считает само, и считает без сети. Всё
  /// остальное про знак — номер, дату, тираж, долю обладателей — знает
  /// только сервер.
  ///
  /// У собирательного знака доля — это доля собранных частей: он не за
  /// величину, а за уже выданные знаки, и мерить его величиной нечем.
  static double progress(
    Sign sign,
    Map<String, int> metrics,
    Set<String> issued,
  ) {
    if (issued.contains(sign.slug)) return 1;
    if (sign.kind == SignKind.collective) {
      if (sign.requires.isEmpty) return 0;
      var have = 0;
      for (final part in sign.requires) {
        if (issued.contains(part)) have++;
      }
      return have / sign.requires.length;
    }
    if (sign.threshold <= 0 || !Metrics.known(sign.metric)) {
      // Знак без годной величины доли не имеет. Ноль, а не единица:
      // единица нарисовала бы полное кольцо у знака, к которому врач не
      // сделал ни шага.
      return 0;
    }
    final value = metrics[sign.metric] ?? 0;
    final share = value / sign.threshold;
    return share > 1 ? 1 : share;
  }
}
