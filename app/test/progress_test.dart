import 'package:curator/progress/metrics.dart';
import 'package:curator/progress/rules.dart';
import 'package:flutter_test/flutter_test.dart';

import 'shared_reference.dart';

/// Сверка расчёта с общим эталоном.
///
/// Эталон — единственное, что держит вместе два расчёта: этот и серверный.
/// Разойдись они — и врач увидит на устройстве один уровень, а в отчёте
/// другой, и кто прав, выяснять будет некому.
void main() {
  final fixtures = readShared('progress-fixtures.json');
  final levels = fixtures['levels'] as Map<String, dynamic>;
  final xp = fixtures['xp'] as Map<String, dynamic>;
  final review = fixtures['review'] as Map<String, dynamic>;
  const rules = Rules.defaults;

  test('правила по умолчанию сходятся с эталоном', () {
    // Числа в коде нужны, чтобы свежая установка работала до первого
    // обращения к серверу. Разойдись они с эталоном — и свежая установка
    // считала бы иначе, чем настроенная.
    expect(rules.thresholds, (levels['thresholds'] as List).cast<int>());
    expect(rules.correct, xp['correct']);
    expect(rules.wrong, xp['wrong']);
    expect(rules.repeatCorrect, xp['repeatCorrect']);
    expect(rules.repeatWrong, xp['repeatWrong']);
    expect(rules.reviewCorrect, xp['reviewCorrect']);
    expect(rules.reviewWrong, xp['reviewWrong']);
    expect(rules.easeStart, review['easeStart']);
    expect(rules.easeFloor, review['easeFloor']);
    expect(rules.firstInterval, review['firstInterval']);
    expect(rules.secondInterval, review['secondInterval']);
  });

  test('уровни сходятся с эталоном', () {
    final cases = levels['cases'] as List;
    expect(cases, isNotEmpty, reason: 'в эталоне нет примеров: сверять нечего');
    for (final one in cases.cast<Map<String, dynamic>>()) {
      expect(
        rules.level(one['xp'] as int),
        one['level'],
        reason: 'при опыте ${one['xp']}',
      );
    }
  });

  test('награда за попытку сходится с эталоном', () {
    final cases = xp['cases'] as List;
    expect(cases, isNotEmpty, reason: 'в эталоне нет примеров: сверять нечего');
    for (final one in cases.cast<Map<String, dynamic>>()) {
      expect(
        rules.award(
          correct: one['correct'] as bool,
          repeat: one['repeat'] as bool,
          review: one['review'] as bool,
        ),
        one['xp'],
        reason:
            'верно=${one['correct']} повтор=${one['repeat']} '
            'интервал=${one['review']}',
      );
    }
  });

  test('повторение сходится с эталоном', () {
    // Здесь и ловится расхождение двух реализаций: SM-2 легко написать
    // «почти так же», и разница вылезет на третьем повторении, через две
    // недели после выкатки.
    final cases = review['cases'] as List;
    expect(cases, isNotEmpty, reason: 'в эталоне нет примеров: сверять нечего');
    final now = DateTime.utc(2026, 9, 19, 12);

    for (final one in cases.cast<Map<String, dynamic>>()) {
      final before = one['before'] as Map<String, dynamic>;
      final after = one['after'] as Map<String, dynamic>;
      final got = rules.next(
        ReviewState(
          ease: (before['ease'] as num).toDouble(),
          intervalDays: before['intervalDays'] as int,
          repetitions: before['repetitions'] as int,
        ),
        one['correct'] as bool,
        now,
      );
      final why = one['why'];
      expect(
        got.ease,
        (after['ease'] as num).toDouble(),
        reason: '$why: лёгкость',
      );
      expect(got.intervalDays, after['intervalDays'], reason: '$why: интервал');
      expect(got.repetitions, after['repetitions'], reason: '$why: повторений');
      // Срок назначается от интервала, а не сам по себе: разойдись они — и
      // задача вернулась бы не тогда, когда обещано.
      expect(
        got.dueAt,
        DateTime.utc(2026, 9, 19 + (after['intervalDays'] as int), 12),
        reason: '$why: срок',
      );
    }
  });

  test('ошибка не стирает накопленную лёгкость', () {
    // Лёгкость — свойство задачи для этого врача, накопленное за месяцы.
    // Сбросить её к началу из-за одной ошибки значит потерять накопленное
    // на пустом месте, и заметить это нельзя ничем, кроме этой проверки.
    final after = rules.next(
      const ReviewState(ease: 1.7, intervalDays: 30, repetitions: 5),
      false,
      DateTime.now(),
    );
    expect(after.ease, isNot(rules.easeStart));
    expect(after.ease, lessThan(1.7));
    expect(after.repetitions, 0);
    expect(after.intervalDays, rules.firstInterval);
  });

  test('лёгкость не проваливается ниже пола', () {
    // На меньшей задача возвращается каждый день и превращает повторение в
    // наказание.
    var state = rules.fresh;
    for (var i = 0; i < 50; i++) {
      state = rules.next(state, false, DateTime.now());
    }
    expect(state.ease, greaterThanOrEqualTo(rules.easeFloor));
  });

  test('каталог величин сходится с эталоном', () {
    final catalog =
        ((fixtures['metrics'] as Map<String, dynamic>)['catalog'] as List)
            .cast<Map<String, dynamic>>();
    expect(catalog, isNotEmpty, reason: 'каталог величин в эталоне пуст');
    expect(Metrics.catalog.length, catalog.length);
    for (var i = 0; i < catalog.length; i++) {
      expect(Metrics.catalog[i].key, catalog[i]['key']);
      expect(Metrics.catalog[i].title, catalog[i]['title']);
    }
    expect(Metrics.known('такой-величины-нет'), isFalse);
  });

  test('недостающая величина приезжает нулём, а не пропуском', () {
    // Пропущенная величина оставила бы кольцо знака пустым у врача,
    // который к нему шёл: сборка переживёт не одну версию сервера.
    final filled = Metrics.fill({'casesSolved': 12});
    expect(filled.length, Metrics.catalog.length);
    expect(filled['casesSolved'], 12);
    expect(filled['daysActive'], 0);
  });
}
