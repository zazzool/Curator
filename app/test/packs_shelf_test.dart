/// Проверки витрины: цена и то, что знает о наборе сервер.
///
/// Проверок виджетов здесь НЕТ, и это не небрежность. Стоит в файле
/// появиться хоть одной `testWidgets`, как Flutter поднимает свою привязку
/// на весь набор, подменяет `HttpClient` и отвечает на всякий запрос кодом
/// 400: настоящий сокет из такого файла недостижим. Экран проверяется
/// отдельно — в `packs_shelf_screen_test.dart`, без сети вовсе.
library;

import 'package:curator/api/client.dart';
import 'package:curator/packs/manifest.dart';
import 'package:curator/packs/shelf.dart';
import 'package:curator/packs/store.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_server.dart';

Release release(String slug, int version) => Release.tryParse({
  'slug': slug,
  'version': version,
  'title': 'Набор',
  'releasedAt': '2026-09-19T10:00:00Z',
  'signature': 'подпись',
  'keyId': 'key-1',
  'cases': <Object?>[],
})!;

void main() {
  group('цена', () {
    test('копейки показываются рублями, и только при показе', () {
      expect(rubles(39000), '390 ₽');
      expect(rubles(39050), '390,50 ₽');
      expect(rubles(5), '0,05 ₽');
      expect(rubles(0), 'Бесплатно');
    });
  });

  group('витрина', () {
    late FakeServer server;
    late Api api;
    late MemoryPackStore store;

    setUp(() async {
      server = await FakeServer.start();
      final tokens = MemoryTokenStore();
      await tokens.write('t-42');
      api = Api(baseUrl: server.origin, appKey: 'app-key-1', tokens: tokens);
      store = MemoryPackStore();
    });

    tearDown(() async {
      api.close();
      await server.stop();
    });

    test(
      'лежащий на устройстве выпуск виден, и сервер про него не знает',
      () async {
        // Что скачано — сведения о телефоне, а не об учётной записи.
        await store.putRelease(release('cardio', 2));
        server.replies.add(
          Reply(200, {
            'packs': [
              {
                'slug': 'cardio',
                'title': 'Кардиология',
                'summaryMd': '',
                'version': 3,
                'cases': 40,
                'kopecks': 39000,
                'owned': true,
              },
            ],
          }),
        );

        final list = await Shelf(api, store).list();

        expect(list.single.installed, 2);
        expect(list.single.isStale, isTrue, reason: 'на сервере выпуск свежее');
        // Сервера про скачанное не спрашивали.
        expect(server.taken.single.path, '/v1/packs');
      },
    );

    test('негодная строка не оставляет врача с пустым экраном', () async {
      server.replies.add(
        Reply(200, {
          'packs': [
            {'slug': 'битый'},
            {
              'slug': 'cardio',
              'title': 'Кардиология',
              'version': 1,
              'cases': 40,
              'kopecks': 0,
              'owned': true,
            },
          ],
        }),
      );

      final list = await Shelf(api, store).list();
      expect(list.length, 1);
      expect(list.single.slug, 'cardio');
    });

    test('пустая витрина — это [], а не отказ', () async {
      server.replies.add(Reply(200, {'packs': <Object?>[]}));
      expect(await Shelf(api, store).list(), isEmpty);
    });
  });
}
