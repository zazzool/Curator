/// Учётная запись врача, какой её видит устройство.
///
/// # Незнакомое отбрасывается, а остальное принимается
///
/// То же правило, что у справочника и у задач: сервер обновляется без
/// приложения, и новое поле в ответе не должно ронять сборку, стоящую на
/// руках. Непонятое право отбрасывается целиком, а не подменяется
/// умолчанием: право с непонятым родом, показанное как подписка, сказало
/// бы врачу, что у него есть доступ ко всему корпусу.
library;

/// Право на доступ: что куплено и до когда.
class Right {
  const Right({
    required this.kind,
    required this.pack,
    required this.origin,
    required this.expiresAt,
  });

  /// 'subscription' — весь корпус, 'pack' — один набор. Словарь закрыт на
  /// сервере, и здесь он закрыт тоже: право с неизвестным родом показать
  /// нечем, и показывать его нельзя.
  final String kind;

  /// Метка набора у права на набор; пусто у подписки.
  final String pack;

  /// Откуда право взялось: покупка, подписка, выдано руками, при
  /// заведении. Показывается врачу словами — он читает это как ответ на
  /// вопрос «за что у меня это есть».
  final String origin;

  /// Пусто — бессрочно. Пустая строка, а не отсутствие: отсутствие поля
  /// означало бы старый сервер, и показывать это надо иначе.
  final String expiresAt;

  bool get forever => expiresAt.isEmpty;

  /// Срок как дата, или пусто, если его нет либо он не разобрался.
  DateTime? get until =>
      expiresAt.isEmpty ? null : DateTime.tryParse(expiresAt);

  static Right? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final kind = raw['kind'];
    if (kind != 'subscription' && kind != 'pack') return null;
    final pack = raw['pack'];
    // Право на набор без набора неисполнимо: показать его значит обещать
    // врачу доступ, которого не выдаст ни одна ручка.
    if (kind == 'pack' && (pack is! String || pack.isEmpty)) return null;
    return Right(
      kind: kind as String,
      pack: pack is String ? pack : '',
      origin: raw['origin'] is String ? raw['origin'] as String : '',
      expiresAt: raw['expiresAt'] is String ? raw['expiresAt'] as String : '',
    );
  }
}

/// Кто я и что у меня есть.
class Profile {
  const Profile({
    required this.accountId,
    required this.email,
    required this.displayName,
    required this.createdAt,
    required this.rights,
    this.dropped = 0,
  });

  final int accountId;

  /// Пусто, пока почта не привязана. Привязки пока нет ни на сервере, ни
  /// здесь, и обещать её экран не должен: обещание, которое некому
  /// исполнить, отправляет врача ждать.
  final String email;

  final String displayName;
  final String createdAt;
  final List<Right> rights;

  /// Сколько прав не разобралось. Считается и показывается: молча
  /// выброшенное право выглядит как «у вас его и не было».
  final int dropped;

  bool get hasSubscription => rights.any((one) => one.kind == 'subscription');

  static Profile parse(Map<String, dynamic> raw) {
    final rights = <Right>[];
    var dropped = 0;
    final list = raw['rights'];
    if (list is List) {
      for (final one in list) {
        final right = Right.tryParse(one);
        if (right == null) {
          dropped++;
        } else {
          rights.add(right);
        }
      }
    }
    return Profile(
      accountId: raw['accountId'] is num
          ? (raw['accountId'] as num).toInt()
          : 0,
      email: raw['email'] is String ? raw['email'] as String : '',
      displayName: raw['displayName'] is String
          ? raw['displayName'] as String
          : '',
      createdAt: raw['createdAt'] is String ? raw['createdAt'] as String : '',
      rights: rights,
      dropped: dropped,
    );
  }
}
