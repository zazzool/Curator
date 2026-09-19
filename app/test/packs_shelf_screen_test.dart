/// Проверки экрана наборов.
///
/// Сети здесь нет вовсе, и не потому, что её лень поднимать: в файле с
/// проверками виджетов Flutter подменяет `HttpClient` и отвечает 400 на
/// всякий запрос. Витрина и закачка приходят на экран готовыми — их и
/// подменяем.
library;

import 'package:curator/api/client.dart';
import 'package:curator/packs/download.dart';
import 'package:curator/packs/manifest.dart';
import 'package:curator/packs/shelf.dart';
import 'package:curator/packs/shelf_screen.dart';
import 'package:curator/packs/store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Витрина, отвечающая заданным.
class StubShelf implements Shelf {
  StubShelf(this.packs, {this.failure});

  final List<ShelfPack> packs;
  final ApiFailure? failure;

  @override
  Future<List<ShelfPack>> list() async {
    final one = failure;
    if (one != null) throw one;
    return packs;
  }
}

/// Закачка, которая ничего не качает.
class StubDownload implements Download {
  StubDownload({this.failure});

  final Object? failure;
  final List<String> asked = [];

  @override
  Future<Downloaded> run(
    String slug, {
    void Function(int done, int total)? onProgress,
  }) async {
    asked.add(slug);
    final one = failure;
    if (one != null) throw one;
    return const Downloaded(saved: 1, total: 1);
  }
}

ShelfPack pack({
  int kopecks = 0,
  bool owned = true,
  int installed = 0,
  int version = 1,
  String summaryMd = '',
}) => ShelfPack(
  slug: 'cardio',
  title: 'Кардиология',
  summaryMd: summaryMd,
  version: version,
  cases: 40,
  kopecks: kopecks,
  owned: owned,
  installed: installed,
);

Widget screen(Shelf shelf, Download download, PackStore store) => MaterialApp(
  home: ShelfScreen(shelf: shelf, download: download, store: store),
);

void main() {
  testWidgets('закрытый набор показывает цену и не предлагает кнопку', (
    tester,
  ) async {
    // Кнопка, которая ничего не покупает, хуже её отсутствия: врач нажмёт
    // и решит, что приложение сломалось.
    await tester.pumpWidget(
      screen(
        StubShelf([pack(kopecks: 39000, owned: false)]),
        StubDownload(),
        MemoryPackStore(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('390 ₽'), findsOneWidget);
    expect(find.textContaining('ещё не открыт'), findsOneWidget);
    expect(find.text('Скачать'), findsNothing);
  });

  testWidgets('открытый и нескачанный набор предлагает скачать', (
    tester,
  ) async {
    final download = StubDownload();
    await tester.pumpWidget(
      screen(
        StubShelf([pack(summaryMd: 'Сорок задач по ЭКГ')]),
        download,
        MemoryPackStore(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('Бесплатно'), findsOneWidget);
    expect(find.text('Сорок задач по ЭКГ'), findsOneWidget);

    await tester.tap(find.text('Скачать'));
    await tester.pumpAndSettle();
    expect(download.asked, ['cardio']);
  });

  testWidgets('скачанный набор говорит, что работает без сети', (tester) async {
    await tester.pumpWidget(
      screen(
        StubShelf([pack(installed: 1)]),
        StubDownload(),
        MemoryPackStore(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('без сети'), findsOneWidget);
    expect(find.text('Убрать'), findsOneWidget);
    expect(find.text('Скачать'), findsNothing);
  });

  testWidgets('выпуск постарше предлагает обновление, а не скачивание', (
    tester,
  ) async {
    await tester.pumpWidget(
      screen(
        StubShelf([pack(installed: 2, version: 3)]),
        StubDownload(),
        MemoryPackStore(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Обновить'), findsOneWidget);
    expect(find.textContaining('выпуск 2'), findsOneWidget);
  });

  testWidgets('не сошедшаяся подпись показывается словами врача', (
    tester,
  ) async {
    // Отказ сверки — не «ошибка 400»: врач должен понять, что набор не
    // поставлен и почему, а не идти переустанавливать приложение.
    await tester.pumpWidget(
      screen(
        StubShelf([pack()]),
        StubDownload(failure: const SignatureFailure()),
        MemoryPackStore(),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Скачать'));
    await tester.pumpAndSettle();

    expect(find.textContaining('проверку подлинности'), findsOneWidget);
  });

  testWidgets('испорченная задача показывается словами врача', (tester) async {
    await tester.pumpWidget(
      screen(
        StubShelf([pack()]),
        StubDownload(failure: const ContentFailure()),
        MemoryPackStore(),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Скачать'));
    await tester.pumpAndSettle();

    expect(find.textContaining('испорченной'), findsOneWidget);
  });

  testWidgets('отказ витрины виден словами, а не пустым экраном', (
    tester,
  ) async {
    await tester.pumpWidget(
      screen(
        StubShelf([], failure: ApiFailure('Не вышло получить наборы')),
        StubDownload(),
        MemoryPackStore(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('Не вышло'), findsOneWidget);
  });

  testWidgets('пустая витрина — исправный случай, а не поломка', (
    tester,
  ) async {
    await tester.pumpWidget(
      screen(StubShelf([]), StubDownload(), MemoryPackStore()),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('Наборов пока нет'), findsOneWidget);
  });

  testWidgets('«Убрать» стирает набор с устройства', (tester) async {
    final store = MemoryPackStore();
    await store.putRelease(
      Release.tryParse({
        'slug': 'cardio',
        'version': 1,
        'title': 'Кардиология',
        'releasedAt': '2026-09-19T10:00:00Z',
        'signature': 'подпись',
        'keyId': 'key-1',
        'cases': <Object?>[],
      })!,
    );

    await tester.pumpWidget(
      screen(StubShelf([pack(installed: 1)]), StubDownload(), store),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Убрать'));
    await tester.pumpAndSettle();

    expect(await store.installed(), isEmpty);
  });
}
