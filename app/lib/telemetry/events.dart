/// Словарь событий телеметрии.
///
/// # Словарь закрыт, и шлёт приложение только его
///
/// Имя, заведённое по месту, сервер отбросит — и отчёт покажет ноль там,
/// где событий были тысячи. Ноль выглядит как данные, и первым это увидит
/// не тот, кто заводил имя.
///
/// Зеркален с `shared/telemetry-events.json`, а через него — с сервером.
/// Проверка отказывает при расхождении и при отсутствии файла.
///
/// # Ответы сюда не едут
///
/// Что врач ответил и верно ли — это попытка, и у неё своя дверь. Пошли мы
/// ответ обоими путями, два счёта решённых задач разошлись бы молча.
library;

/// Событие словаря.
class TelemetryEvent {
  const TelemetryEvent(this.name, this.title, this.props);

  final String name;
  final String title;

  /// Разрешённые свойства. Сервер отбрасывает остальные вместе со
  /// значением, так что свойство, посланное мимо словаря, просто исчезает.
  final List<String> props;
}

/// Словарь целиком.
///
/// Порядок значим: он же порядок разделов в отчётах, и в нём читается путь
/// врача — от запуска к разбору, от разбора к набору, от набора к покупке.
abstract final class Events {
  static const catalog = <TelemetryEvent>[
    TelemetryEvent('app_opened', 'Запуск приложения', ['cold']),
    TelemetryEvent('session_ended', 'Конец захода', ['spentMs', 'cases']),
    TelemetryEvent('case_shown', 'Задача показана', ['caseId', 'mode']),
    TelemetryEvent('case_skipped', 'Задача пропущена', ['caseId', 'mode']),
    TelemetryEvent('explanation_opened', 'Открыт разбор', ['caseId']),
    TelemetryEvent('fragment_opened', 'Открыт фрагмент условия', [
      'caseId',
      'fragment',
    ]),
    TelemetryEvent('review_started', 'Начато повторение', ['due']),
    TelemetryEvent('review_finished', 'Повторение доведено', ['done', 'due']),
    TelemetryEvent('pack_opened', 'Открыт набор на витрине', ['pack']),
    TelemetryEvent('pack_download_started', 'Начата закачка набора', [
      'pack',
      'version',
    ]),
    TelemetryEvent('pack_download_finished', 'Набор докачан', [
      'pack',
      'version',
      'spentMs',
    ]),
    TelemetryEvent('pack_download_failed', 'Закачка набора сорвалась', [
      'pack',
      'version',
      'reason',
    ]),
    TelemetryEvent('paywall_shown', 'Показана цена закрытого набора', ['pack']),
    TelemetryEvent('purchase_started', 'Нажата покупка', ['purpose']),
    TelemetryEvent('purchase_finished', 'Покупка состоялась', ['purpose']),
    TelemetryEvent('sign_seen', 'Врач увидел знак', ['sign']),
    TelemetryEvent('level_seen', 'Врач увидел новый уровень', ['level']),
  ];

  static TelemetryEvent? known(String name) {
    for (final one in catalog) {
      if (one.name == name) return one;
    }
    return null;
  }
}
