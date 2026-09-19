import 'package:curator/progress/progress_screen.dart';
import 'package:curator/progress/state.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Разбор прогресса и знаков и то, как они выглядят.
///
/// Знаки считает сервер: номер, тираж и доля обладателей — сведения о том,
/// каким врач пришёл среди всех, и придумать их приложение не может. Здесь
/// проверяется, что оно их не придумывает и не теряет.
void main() {
  group('разбор', () {
    test('величины приезжают полным набором, даже когда их не прислали', () {
      // Отсутствующая строка оставила бы кольцо знака пустым у врача,
      // который к нему шёл.
      final one = Standing.parse({'xp': 120, 'level': 2, 'due': 3});
      expect(one.xp, 120);
      expect(one.level, 2);
      expect(one.due, 3);
      expect(one.metrics['casesSolved'], 0);
      expect(one.metrics.length, greaterThan(1));
    });

    test('уровень ниже первого не показывается', () {
      // Нулевой уровень читается как «ты никто», и это первое, что человек
      // увидел бы.
      expect(Standing.parse(const {}).level, 1);
    });

    test(
      'знак без метки или названия отбрасывается один, а не весь список',
      () {
        expect(
          SignStanding.tryParse(const {'slug': '', 'title': 'Знак'}),
          isNull,
        );
        expect(SignStanding.tryParse(const {'slug': 'a', 'title': ''}), isNull);
        expect(
          SignStanding.tryParse(const {'slug': 'a', 'title': 'Знак'}),
          isNotNull,
        );
      },
    );

    test('доля пути не выходит за границы', () {
      final one = SignStanding.tryParse(const {
        'slug': 'a',
        'title': 'Знак',
        'progress': 4.2,
      })!;
      expect(one.progress, 1);
    });
  });

  group('показ знака', () {
    Future<void> show(WidgetTester tester, SignStanding sign) =>
        tester.pumpWidget(
          MaterialApp(
            home: Scaffold(body: SignRow(sign: sign)),
          ),
        );

    testWidgets('выданный знак называет номер и тираж', (tester) async {
      await show(
        tester,
        const SignStanding(
          slug: 'pioneer',
          title: 'Первопроходец',
          kind: 'edition',
          issued: true,
          progress: 1,
          serial: 7,
          editionSize: 100,
        ),
      );
      expect(find.text('№ 7 из 100'), findsOneWidget);
    });

    testWidgets('невыданный знак с тиражом говорит, сколько осталось', (
      tester,
    ) async {
      // Знак, который можно заслужить и не получить, обязан сказать об
      // этом до того, как тираж кончится.
      await show(
        tester,
        const SignStanding(
          slug: 'pioneer',
          title: 'Первопроходец',
          kind: 'edition',
          issued: false,
          progress: 0.5,
          issuedCount: 90,
          editionSize: 100,
        ),
      );
      expect(find.textContaining('осталось в тираже 10'), findsOneWidget);
    });

    testWidgets('отозванный знак остаётся в списке с отметкой', (tester) async {
      // Строка о выдаче не удаляется никогда, и исчезнувший из списка знак
      // врач прочёл бы как поломку.
      await show(
        tester,
        const SignStanding(
          slug: 'primus',
          title: 'Primus inter pares',
          kind: 'rotating',
          issued: true,
          progress: 1,
          revoked: true,
          serial: 1,
        ),
      );
      expect(find.textContaining('перешёл другому'), findsOneWidget);
    });
  });

  group('экран', () {
    testWidgets('величины видны все, включая нулевые', (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(body: StandingCard(standing: Standing.empty)),
        ),
      );
      // У врача, ещё ничего не решавшего, они нули, а не отсутствующие
      // строки: иначе не видно, к чему вообще можно идти.
      expect(find.text('Решено задач'), findsOneWidget);
      expect(find.text('Источников затронуто'), findsOneWidget);
      expect(find.text('Уровень 1'), findsOneWidget);
    });
  });
}
