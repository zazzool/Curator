import 'package:curator/telemetry/events.dart';
import 'package:flutter_test/flutter_test.dart';

import 'shared_reference.dart';

/// Сверка словаря событий с общим эталоном.
///
/// Имя, заведённое по месту, сервер отбросит — и отчёт покажет ноль там,
/// где событий были тысячи. Ноль выглядит как данные.
void main() {
  final events = ((readShared('telemetry-events.json'))['events'] as List)
      .cast<Map<String, dynamic>>();

  test('словарь событий сходится с эталоном', () {
    expect(events, isNotEmpty, reason: 'в эталоне нет событий: сверять нечего');
    expect(Events.catalog.length, events.length);
    for (var i = 0; i < events.length; i++) {
      // Порядок сверяется тоже: он же порядок разделов в отчётах, и в нём
      // читается путь врача.
      expect(Events.catalog[i].name, events[i]['name'], reason: 'событие $i');
      expect(
        Events.catalog[i].title,
        events[i]['title'],
        reason: 'событие ${Events.catalog[i].name}',
      );
      expect(
        Events.catalog[i].props,
        (events[i]['props'] as List).cast<String>(),
        reason: 'событие ${Events.catalog[i].name}',
      );
    }
    expect(Events.known('такого-события-нет'), isNull);
  });

  test('ответ не едет телеметрией', () {
    // Что врач ответил и верно ли — это попытка, и у неё своя дверь.
    // Пошли мы ответ обоими путями, два счёта решённых задач разошлись бы
    // молча.
    for (final one in Events.catalog) {
      expect(one.props, isNot(contains('correct')), reason: one.name);
      expect(one.props, isNot(contains('answer')), reason: one.name);
    }
  });
}
