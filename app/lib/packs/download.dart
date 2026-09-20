/// Закачка набора на устройство.
///
/// # Порядок действий здесь — это и есть защита
///
/// Сперва опись и её подпись, потом задачи, и только потом опись
/// записывается на устройство. Переставь любые два шага — и защита
/// исчезает, хотя код выглядит работающим:
///
/// - скачай мы задачи до сверки подписи, подменённый набор лёг бы на
///   устройство и был бы отброшен уже после того, как врач по нему
///   позанимался;
/// - запиши мы опись до задач, оборванная закачка выдала бы себя за целую,
///   и врач увидел бы набор с дырами, решив, что задач просто нет.
///
/// # Отпечаток сверяется у каждой задачи
///
/// Не один на весь набор: задачи качаются по одной, и проверка «весь набор
/// целиком» на обрыве посреди закачки не говорит ничего. Отпечатки взяты
/// из описи, а опись подписана, — значит, подменить задачу по дороге,
/// не сломав подписи, нельзя.
///
/// # Оборванная закачка продолжается, а не начинается заново
///
/// Сервер отдаёт задачи по возрастанию номера и принимает курсор. Задачи
/// ложатся на устройство по мере приезда, поэтому уже лежащее — это всегда
/// начало списка, и продолжить можно с наибольшего из лежащих. Метро
/// кончается раньше, чем набор.
library;

import '../api/client.dart';
import 'manifest.dart';
import 'store.dart';

/// Чем кончилась закачка.
class Downloaded {
  const Downloaded({required this.saved, required this.total});

  /// Сколько задач приехало в этот раз. Нуль при целом наборе — исправный
  /// случай: качать было нечего.
  final int saved;

  final int total;
}

/// Задача, не сошедшаяся с описью.
///
/// Отдельный род отказа: «нет сети» — это «попробуйте позже», а не
/// сошедшийся отпечаток значит, что набору верить нельзя.
class ContentFailure implements Exception {
  const ContentFailure([
    this.message =
        'Часть набора приехала испорченной и не была сохранена. '
        'Попробуйте позже',
  ]);

  final String message;

  @override
  String toString() => message;
}

/// Закачка наборов.
class Download {
  Download(this._api, this._store, this._keys);

  final Api _api;
  final PackStore _store;
  final TrustedKeys _keys;

  /// Сколько задач просится одной страницей.
  ///
  /// Не весь набор: набор на тысячу задач весит мегабайты, и телефон в
  /// метро не дотянет одну большую закачку. Сервер режет и сам, на двухстах.
  static const page = 50;

  /// Ставит набор. Сообщает о ходе через [onProgress], если он задан.
  Future<Downloaded> run(
    String slug, {
    void Function(int done, int total)? onProgress,
  }) async {
    final raw = await _api.get('/v1/packs/$slug');
    final release = Release.tryParse(raw);
    if (release == null) {
      // Непонятая опись — это не «поставим, что разобралось»: набор с
      // дырой хуже его отсутствия, врач решит, что задач просто нет.
      throw const ContentFailure(
        'Опись набора приехала непонятной. Попробуйте позже',
      );
    }
    // Подпись сверяется ДО первой скачанной задачи: иначе подменённый
    // набор успел бы лечь на устройство.
    await verifyRelease(release, _keys);

    // Пришёл ли выпуск того набора, который просили.
    //
    // Подпись метку покрывает, значит подделать её нельзя. Но верно
    // подписанный выпуск набора А принимается в ответ на просьбу о
    // наборе Б — хватит ошибки сервера, переименования или псевдонима.
    // Дальше задачи легли бы под запрошенной меткой, а опись под
    // пришедшей: `release(slug)` возвращал бы пусто навсегда, витрина
    // вечно предлагала бы «Установить», а офлайновая лента этих задач не
    // видела бы. Врач качал бы набор снова и снова, и работа без сети
    // просто не включилась бы.
    if (release.slug != slug) {
      throw const ContentFailure(
        'Сервер прислал опись не того набора. Попробуйте позже',
      );
    }

    // Откатить выпуск назад нельзя: старый выпуск — это старые ответы.
    //
    // Сравнение версий есть в витрине, но только ДЛЯ ПОКАЗА: оно решает,
    // рисовать ли «Обновить», и ничего не запрещает. Верно подписанный
    // старый выпуск лёг бы поверх нового молча.
    //
    // Отказа наружу нет намеренно: ставить нечего, и сказать об этом
    // врачу — значит объявить отказом то, что у него уже всё есть.
    final installed = await _store.release(slug);
    if (installed != null && release.version < installed.version) {
      return Downloaded(saved: 0, total: installed.cases.length);
    }

    final wanted = {for (final one in release.cases) one.id: one.hash};
    // Лежащее могло остаться от прежнего выпуска: состав меняется, и
    // задача, выкинутая из набора, отпечатка в новой описи не имеет.
    final stored = (await _store.have(slug)).where(wanted.containsKey).toSet();

    // Курсор ставится на конец СПЛОШНОГО начала, а не на наибольшее из
    // лежащего. Разница не умозрительная: после смены состава в лежащем
    // может оказаться дыра, и курсор по наибольшему перепрыгнул бы её —
    // набор не докачался бы никогда, сколько ни повторяй.
    final order = wanted.keys.toList()..sort();
    var solid = 0;
    while (solid < order.length && stored.contains(order[solid])) {
      solid++;
    }
    var after = solid == 0 ? '' : order[solid - 1];

    var saved = 0;
    onProgress?.call(stored.length, wanted.length);

    while (stored.length < wanted.length) {
      final reply = await _api.get(
        '/v1/packs/$slug/cases',
        query: {'limit': '$page', if (after.isNotEmpty) 'after': after},
      );
      final list = reply['cases'];
      if (list is! List || list.isEmpty) break;

      for (final one in list) {
        if (one is! Map) throw const ContentFailure();
        final id = one['id'];
        if (id is! String || id.isEmpty) throw const ContentFailure();
        after = id;

        final promised = wanted[id];
        if (promised == null) {
          // Задача, которой в подписанной описи нет. Не отказ всему
          // набору: состав мог смениться между описью и этой страницей,
          // и лишнее просто не берётся.
          continue;
        }
        if (stored.contains(id)) continue;

        final body = one['body'];
        if (await hashBody(body) != promised) {
          // Ради этого всё и написано: отпечаток из подписанной описи —
          // единственное, что отличает задачу от подменённой по дороге.
          throw const ContentFailure();
        }
        if (!safeName(id)) throw const ContentFailure();
        await _store.putCase(slug, id, body);
        stored.add(id);
        saved++;
        onProgress?.call(stored.length, wanted.length);
      }

      final next = reply['next'];
      if (next is! String || next.isEmpty) break;
      after = next;
    }

    if (stored.length < wanted.length) {
      // Недосчитались задач — набор не ставится. Оставленное на
      // устройстве не пропадает: следующая попытка продолжит с того же
      // места, а опись так и не ляжет, и набор не выдаст себя за целый.
      throw const ContentFailure(
        'Набор приехал не целиком. Проверьте связь и попробуйте ещё раз',
      );
    }

    // Опись ложится последней: она и есть отметка «набор стоит целиком».
    await _store.putRelease(release);
    return Downloaded(saved: saved, total: wanted.length);
  }
}
