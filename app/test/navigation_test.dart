/// Проверки устройства интерфейса: состав разделов и дороги между ними.
///
/// Проверяется здесь не оформление, а то, что врач видит и куда может
/// дойти: сколько разделов в нижнем меню и как они названы, что «Практика»
/// предлагает режим, а не список задач, и что значок настроек ведёт в
/// «Настройки» со всеми тремя группами.
///
/// Сети здесь нет вовсе: в файле с проверками виджетов Flutter подменяет
/// `HttpClient` и отвечает 400 на всякий запрос — молча. Витрина наборов
/// поэтому не доедет, и это часть проверки: экран обязан работать и так.
library;

import 'package:curator/api/client.dart';
import 'package:curator/cases/outbox.dart';
import 'package:curator/core/app_scope.dart';
import 'package:curator/core/ui/search_field.dart';
import 'package:curator/core/design/app_theme.dart';
import 'package:curator/home.dart';
import 'package:curator/packs/manifest.dart';
import 'package:curator/packs/store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Widget app() {
  final api = Api(
    baseUrl: Uri.parse('http://127.0.0.1:1'),
    appKey: '',
    tokens: MemoryTokenStore(),
  );
  final outbox = Outbox(api, MemoryOutboxStore());
  final packs = MemoryPackStore();
  final keys = TrustedKeys.parse('');
  // Область со службами — над `MaterialApp`, как в `main()`: экраны,
  // открытые поверх раздела, живут в навигаторе, и область ниже им не
  // видна. Собери проверка дерево иначе, она проверяла бы не то
  // приложение, которое ставится врачу.
  return AppScope(
    api: api,
    outbox: outbox,
    packs: packs,
    keys: keys,
    // Местной базы нет: разделы обязаны открыться и объяснить, почему
    // пусто, а не исчезнуть из полосы.
    schedule: null,
    reference: null,
    accountEpoch: ValueNotifier<int>(0),
    child: MaterialApp(
      theme: AppTheme.light(),
      home: Home(
        api: api,
        outbox: outbox,
        packs: packs,
        keys: keys,
        schedule: null,
        reference: null,
      ),
    ),
  );
}

void main() {
  testWidgets('нижнее меню — три раздела и названы они по-донорски', (
    tester,
  ) async {
    await tester.pumpWidget(app());
    await tester.pumpAndSettle();

    expect(find.byType(NavigationDestination), findsNWidgets(3));
    // «Практика» и «Прогресс» встречаются по одному разу — только в
    // полосе: содержание невыбранных разделов `IndexedStack` прячет.
    expect(find.text('Практика'), findsOneWidget);
    expect(find.text('Прогресс'), findsOneWidget);

    // Разделов, которые мерке не отвечают, в полосе нет: повторение —
    // режим практики, каталог наборов — витрина за настройками.
    expect(find.text('Наборы'), findsNothing);
    expect(find.text('Знаки'), findsNothing);

    // «Теория» открыта первой и объясняет, почему пуста, а не исчезает:
    // исчезнувший раздел врач ищет как поломку. Поля поиска при этом нет
    // — пустое поле обещает выдачу, которой не будет.
    expect(find.text('Теория недоступна'), findsOneWidget);
    expect(find.byType(AppSearchField), findsNothing);
  });

  testWidgets('«Практика» предлагает режимы, а не список задач', (
    tester,
  ) async {
    await tester.pumpWidget(app());
    await tester.pumpAndSettle();

    await tester.tap(find.text('Практика'));
    await tester.pumpAndSettle();

    expect(find.text('Новая задача'), findsOneWidget);
    // Повторение — плитка, и без пройденных задач она недоступна: подпись
    // говорит, откуда оно возьмётся, а не молчит.
    expect(find.text('Повторение'), findsOneWidget);
    expect(
      find.text('Появится после первой пройденной задачи'),
      findsOneWidget,
    );
  });

  testWidgets('значок настроек ведёт в «Настройки» с тремя группами', (
    tester,
  ) async {
    await tester.pumpWidget(app());
    await tester.pumpAndSettle();

    await tester.tap(find.text('Практика'));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.settings_outlined));
    await tester.pumpAndSettle();

    expect(find.text('Настройки'), findsOneWidget);
    // Заголовки групп набраны прописными самим `SectionLabel`.
    expect(find.text('НАБОРЫ ЗАДАЧ'), findsOneWidget);
    expect(find.text('Каталог наборов'), findsOneWidget);
    expect(find.text('УЧЁТНАЯ ЗАПИСЬ'), findsOneWidget);
    expect(find.text('Учётная запись'), findsOneWidget);
    expect(find.text('ПРИЛОЖЕНИЕ'), findsOneWidget);
    expect(find.text('О приложении'), findsOneWidget);
  });
}
