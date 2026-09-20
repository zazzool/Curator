/// Службы приложения там, где их некому передать.
///
/// Значок настроек стоит в шапке каждого корневого экрана и ведёт в одно
/// место. У донора это ничего не стоит: экраны живут на маршрутизаторе, а
/// службы берутся провайдерами — значку довольно имени маршрута. Здесь ни
/// маршрутизатора, ни провайдеров нет, и заводить их ради одного значка
/// было бы зависимостью без нужды: экранов, которым надо дотянуться до
/// служб мимо своего родителя, три, и все три открываются поверх раздела.
///
/// Поэтому службы кладутся над нижним меню и читаются по месту. Читаются
/// через [of], а не через `maybeOf`: экран, открытый мимо области,
/// обязан упасть при сборке, а не показать пустой список задач — пустой
/// список врач принимает за правду.
library;

import 'package:flutter/widgets.dart';

import '../api/client.dart';
import '../cases/outbox.dart';
import '../db/reference_store.dart';
import '../db/schedule.dart';
import '../packs/manifest.dart';
import '../packs/store.dart';

class AppScope extends InheritedWidget {
  const AppScope({
    super.key,
    required this.api,
    required this.outbox,
    required this.packs,
    required this.keys,
    required this.schedule,
    required this.reference,
    required this.accountEpoch,
    required super.child,
  });

  final Api api;
  final Outbox outbox;
  final PackStore packs;

  /// Открытые ключи, которым верит эта сборка.
  final TrustedKeys keys;

  /// Расписание повторения на устройстве. Пусто — местная база не
  /// открылась, и повторения без сети не будет.
  final Schedule? schedule;

  /// Справочник на устройстве. Пусто по той же причине.
  final ReferenceStore? reference;

  /// Счётчик смен учётной записи: растёт, когда врач вернул себе доступ
  /// по почте, и разделы по нему перечитываются.
  ///
  /// Возврат доступа меняет не один экран: лента, повторение и знаки
  /// показывают прежнюю запись, а возврат из «Настроек» приходит только
  /// в тот раздел, откуда их открыли. Объявить смену надо один раз на всё
  /// приложение — иначе врач увидит чужие числа под своей вернувшейся
  /// почтой.
  final ValueNotifier<int> accountEpoch;

  static AppScope of(BuildContext context) {
    final scope = context.dependOnInheritedWidgetOfExactType<AppScope>();
    assert(scope != null, 'экран открыт мимо AppScope');
    return scope!;
  }

  @override
  bool updateShouldNotify(AppScope old) =>
      api != old.api ||
      outbox != old.outbox ||
      packs != old.packs ||
      schedule != old.schedule ||
      reference != old.reference ||
      accountEpoch != old.accountEpoch;
}
