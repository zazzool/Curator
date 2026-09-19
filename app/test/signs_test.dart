import 'package:curator/signs/catalog.dart';
import 'package:flutter_test/flutter_test.dart';

import 'shared_reference.dart';

/// Сверка каталога знаков с общим эталоном.
///
/// Знак выдаёт сервер, а показывает приложение, и показать оно обязано то
/// же самое. Два каталога, набранных порознь, расходятся молча — и первым
/// это увидит врач, у которого на экране знак называется иначе, чем в
/// свидетельстве.
void main() {
  final shared = readShared('signs-catalog.json');
  final catalog = (shared['catalog'] as List).cast<Map<String, dynamic>>();
  final kinds = ((shared['kinds'] as Map<String, dynamic>)['list'] as List)
      .cast<String>();

  test('виды знаков сходятся с эталоном', () {
    // Вид, заведённый по месту, приложение не нарисует, а сервер выдаст —
    // и знак повиснет невидимым.
    expect(SignKind.values.map((one) => one.name).toList(), kinds);
    expect(SignKind.parse('такого-вида-нет'), isNull);
  });

  test('каталог знаков сходится с эталоном', () {
    expect(catalog, isNotEmpty, reason: 'в эталоне нет знаков: сверять нечего');
    expect(Signs.catalog.length, catalog.length);
    for (var i = 0; i < catalog.length; i++) {
      final want = catalog[i];
      final got = Signs.catalog[i];
      // Порядок сверяется тоже: знаки показываются списком, и порядок в
      // нём — часть того, что человек видит.
      expect(got.slug, want['slug'], reason: 'знак $i');
      expect(got.title, want['title'], reason: 'знак ${got.slug}');
      expect(got.kind.name, want['kind'], reason: 'знак ${got.slug}');
      expect(got.metric, want['metric'] ?? '', reason: 'знак ${got.slug}');
      expect(got.threshold, want['threshold'] ?? 0, reason: 'знак ${got.slug}');
      expect(
        got.requires,
        (want['requires'] as List? ?? []).cast<String>(),
        reason: 'знак ${got.slug}',
      );
      expect(
        got.editionSize,
        want['editionSize'] ?? 0,
        reason: 'знак ${got.slug}',
      );
      expect(got.xp, want['xp'] ?? 0, reason: 'знак ${got.slug}');
    }
  });

  test('доля пути считается по величине и не переваливает за единицу', () {
    final sign = Signs.known('first-steps')!;
    expect(Signs.progress(sign, {'casesSolved': 0}, {}), 0);
    expect(Signs.progress(sign, {'casesSolved': 5}, {}), 0.5);
    expect(Signs.progress(sign, {'casesSolved': 10}, {}), 1);
    // Больше порога — всё равно единица: кольцо не бывает полнее полного.
    expect(Signs.progress(sign, {'casesSolved': 400}, {}), 1);
  });

  test('выданный знак полон, чем бы его ни мерили', () {
    // Знак с тиражом можно заслужить и не получить, но полученный — полон
    // всегда: показать врачу неполное кольцо у знака, который лежит у него
    // в свидетельстве, нечем.
    final sign = Signs.known('pioneer')!;
    expect(Signs.progress(sign, {}, {'pioneer'}), 1);
  });

  test('доля собирательного знака — доля собранных частей', () {
    // Он не за величину, а за уже выданные знаки, и мерить его величиной
    // нечем.
    final sign = Signs.known('order-of-accuracy')!;
    expect(sign.requires.length, 2);
    expect(Signs.progress(sign, {'casesSolved': 100000}, {}), 0);
    expect(Signs.progress(sign, {}, {'hundred'}), 0.5);
    expect(Signs.progress(sign, {}, {'hundred', 'streak-25'}), 1);
  });

  test('знак с неизвестной величиной доли не имеет', () {
    // Ноль, а не единица: единица нарисовала бы полное кольцо у знака, к
    // которому врач не сделал ни шага.
    const sign = Sign(
      slug: 'выдуманный',
      title: 'Выдуманный',
      kind: SignKind.metric,
      metric: 'такой-величины-нет',
      threshold: 10,
    );
    expect(Signs.progress(sign, {'такой-величины-нет': 10}, {}), 0);
  });
}
