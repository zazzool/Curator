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

    test('линейка и довод доезжают, незнакомые отбрасываются', () async {
      // Незнакомое не подменяется умолчанием: старое приложение получит
      // однажды линейку, которой не знает, и соврать про способ оплаты
      // хуже, чем промолчать о нём.
      server.replies.add(
        Reply(200, {
          'packs': [
            {
              'slug': 'cardio',
              'title': 'Кардиология',
              'version': 1,
              'cases': 40,
              'kopecks': 39000,
              'owned': true,
              'line': 'paid',
              'openedBy': 'group',
            },
            {
              'slug': 'neuro',
              'title': 'Неврология',
              'version': 1,
              'cases': 10,
              'kopecks': 0,
              'owned': false,
              'line': 'besplatno',
              'openedBy': 'по знакомству',
            },
          ],
        }),
      );

      final list = await Shelf(api, store).list();
      expect(list.first.line, PackLine.paid);
      expect(list.first.openedBy, OpenedBy.group);
      expect(list.last.line, '');
      expect(list.last.openedBy, '');
    });

    test('набор без линейки не врёт про способ оплаты', () async {
      // Старый сервер линейки не присылает вовсе, и приложение обязано
      // это пережить: строка на месте, набор в списке, способ не назван.
      server.replies.add(
        Reply(200, {
          'packs': [
            {
              'slug': 'cardio',
              'title': 'Кардиология',
              'version': 1,
              'cases': 40,
              'kopecks': 0,
              'owned': false,
            },
          ],
        }),
      );

      final list = await Shelf(api, store).list();
      expect(list.single.line, '');
      expect(closedNote(list.single.line), 'Набор пока закрыт');
    });
  });

  group('что сделать с закрытым набором', () {
    test('каждая линейка говорит своё, и ни одна не молчит', () {
      // Закрытый набор без причины — тупик: врач не знает, войти ему,
      // привязать почту или ждать оператора, и одинаково часто не делает
      // ничего.
      expect(closedNote(PackLine.basic), contains('почту'));
      expect(closedNote(PackLine.paid), contains('подпиской'));
      expect(closedNote(PackLine.guest), isNotEmpty);
      expect(closedNote(PackLine.sponsored), isNotEmpty);
    });

    test('про оплату говорит только платная линейка', () {
      // Врачу, которому достаточно привязать почту, обещание «появится,
      // как только будет оплачен» — прямая неправда. Она здесь и стояла,
      // одна на все линейки сразу.
      for (final line in [
        PackLine.basic,
        PackLine.guest,
        PackLine.sponsored,
        '',
      ]) {
        expect(
          closedNote(line).toLowerCase(),
          isNot(anyOf(contains('оплач'), contains('куп'))),
          reason: 'линейка $line не продаётся, а обещает оплату',
        );
      }
    });
  });
}
