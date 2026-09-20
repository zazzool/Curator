import 'package:curator/core/design/app_theme.dart';
import 'package:curator/core/design/palette.dart';
import 'package:curator/core/design/typography.dart';
import 'package:curator/core/ui/surface.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Оформление: то в нём, что расходится молча.
///
/// Сверить вид с донором построчно нельзя — донор живёт в другом
/// репозитории, и проверка, читающая чужое дерево, на стороже не поднялась
/// бы вовсе. Поэтому здесь стережётся не сходство картинок, а те правила,
/// нарушение которых не даёт ни отказа, ни красного: тема без палитры,
/// полужирное без оси вариативного шрифта, тень вместо волосяной границы.
void main() {
  group('тема', () {
    test('несёт палитру расширением, а не только цветами ролей', () {
      // Без расширения `context.palette` уходит на запасной путь и
      // отдаёт светлую палитру под тёмной темой — то есть чернила по
      // чернилам. Отказа при этом нет никакого.
      final light = AppTheme.light().extension<AppPalette>();
      final dark = AppTheme.dark().extension<AppPalette>();
      expect(light, AppPalette.light);
      expect(dark, AppPalette.dark);
    });

    test('фон экрана взят у палитры, а не у схемы', () {
      expect(AppTheme.light().scaffoldBackgroundColor, AppPalette.light.canvas);
      expect(AppTheme.dark().scaffoldBackgroundColor, AppPalette.dark.canvas);
    });

    test('теней нет ни у карточек, ни у полос', () {
      // Тень допустима только у того, что физически висит над страницей.
      // Ненулевая высота возвращается сама при обновлении Flutter: у
      // Material по умолчанию она не ноль.
      final theme = AppTheme.light();
      expect(theme.cardTheme.elevation, 0);
      expect(theme.appBarTheme.elevation, 0);
      expect(theme.appBarTheme.scrolledUnderElevation, 0);
      expect(theme.navigationBarTheme.elevation, 0);
    });

    test('шрифт один на всё приложение', () {
      expect(AppTheme.light().textTheme.bodyMedium?.fontFamily, AppType.family);
    });
  });

  group('вариативный шрифт', () {
    // Golos Text — вариативный, ось начертания одна на весь файл, и
    // `fontWeight` её не двигает. Стиль, у которого задан только вес,
    // рисуется обычным начертанием — молча.
    void bothSet(TextStyle style, String name) {
      expect(style.fontWeight, isNotNull, reason: '$name: не задан вес');
      expect(
        style.fontVariations,
        isNotEmpty,
        reason: '$name: вес задан, а ось вариативного шрифта — нет',
      );
    }

    test('у всех начертаний каталога задана ось, а не только вес', () {
      bothSet(AppType.display, 'display');
      bothSet(AppType.titleL, 'titleL');
      bothSet(AppType.titleM, 'titleM');
      bothSet(AppType.titleS, 'titleS');
      bothSet(AppType.body, 'body');
      bothSet(AppType.reading, 'reading');
      bothSet(AppType.bodyStrong, 'bodyStrong');
      bothSet(AppType.label, 'label');
      bothSet(AppType.caption, 'caption');
      bothSet(AppType.overline, 'overline');
      bothSet(AppType.numeral, 'numeral');
      bothSet(AppType.numeralHero, 'numeralHero');
      bothSet(AppType.caseNumber, 'caseNumber');
    });
  });

  group('палитра', () {
    testWidgets('без расширения берётся по яркости, а не роняет экран', (
      tester,
    ) async {
      // Виджет могут показать под голым MaterialApp — так делают и сами
      // проверки экранов. Отсутствие токенов не повод падать.
      late AppPalette seen;
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData(brightness: Brightness.dark),
          home: Builder(
            builder: (context) {
              seen = context.palette;
              return const SizedBox.shrink();
            },
          ),
        ),
      );
      expect(seen, AppPalette.dark);
    });

    test('красный занят строго неверным ответом', () {
      // Успех оливковый, а не зелёный, и не совпадает с опасностью: цвет
      // различают не все, и совпади эти два — разбор задачи у части
      // врачей перестал бы читаться вовсе.
      expect(AppPalette.light.success, isNot(AppPalette.light.danger));
      expect(AppPalette.dark.success, isNot(AppPalette.dark.danger));
    });

    test('чернила тёмной зоны светлые в обеих темах', () {
      // Зона тёмная при любой теме системы, и обычные чернила на ней не
      // читаются. Подставить их «пока сойдёт» нельзя: не читаются они
      // ровно у того, кто зону открыл.
      for (final p in [AppPalette.light, AppPalette.dark]) {
        expect(p.heroInk.computeLuminance(), greaterThan(0.5));
        expect(p.heroCanvas.computeLuminance(), lessThan(0.2));
      }
    });
  });

  group('общие части', () {
    testWidgets('у пустого состояния есть выход, когда его дали', (
      tester,
    ) async {
      var pressed = 0;
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: Scaffold(
            body: EmptyState(
              icon: const Icon(Icons.inbox_outlined),
              title: 'Здесь пусто',
              description: 'И это исправный случай',
              action: OutlinedButton(
                onPressed: () => pressed++,
                child: const Text('Обновить'),
              ),
            ),
          ),
        ),
      );
      expect(find.text('Здесь пусто'), findsOneWidget);
      await tester.tap(find.text('Обновить'));
      expect(pressed, 1);
    });

    testWidgets('цель нажатия значка в шапке не меньше 48 точек', (
      tester,
    ) async {
      // Меньшая цель проходит глазами и не проходит пальцем; 48 точек —
      // предел снизу из руководства по доступности, а не высота.
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: Scaffold(
            body: ScreenHeader(
              title: 'Задачи',
              onBack: () {},
              actions: [
                ScreenHeaderAction(
                  icon: Icons.settings_outlined,
                  label: 'Настройки',
                  onTap: () {},
                ),
              ],
            ),
          ),
        ),
      );
      // Меряется отрисованное, а не объявленное: значок внутри сам по
      // себе мельче цели, и проверка по вложенным коробкам поймала бы
      // его, а не цель.
      for (final at in find.byType(ScreenHeaderAction).evaluate()) {
        final size = tester.getSize(find.byElementPredicate((e) => e == at));
        expect(size.width, greaterThanOrEqualTo(minTouchTarget));
        expect(size.height, greaterThanOrEqualTo(minTouchTarget));
      }
      expect(find.byType(ScreenHeaderAction), findsNWidgets(2));
    });

    testWidgets('длинная подпись таблетки обрезается, а не рвёт полосу', (
      tester,
    ) async {
      // Подписи приходят словами источника, а источник любой:
      // «дифференциальный диагноз» длиннее узкого экрана при крупном
      // системном шрифте. Без гибкой подписи здесь была бы полоса отказа.
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: const Scaffold(
            body: SizedBox(
              width: 120,
              child: Wrap(
                children: [
                  Pill(
                    label: 'дифференциальный диагноз: 12',
                    icon: Icon(Icons.checklist_outlined),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
      expect(tester.takeException(), isNull);
    });
  });
}
