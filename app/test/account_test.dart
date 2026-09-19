import 'package:curator/account/model.dart';
import 'package:flutter_test/flutter_test.dart';

/// Учётная запись и права врача.
///
/// Право выдаётся платежом и живёт само, чем бы ни было куплено. Здесь
/// проверяется, что приложение не выдумывает прав и не теряет их молча:
/// показанное право, которого нет, обещает доступ, а потерянное выглядит
/// как «вы не платили».
void main() {
  group('разбор прав', () {
    test('подписка со сроком разбирается целиком', () {
      final one = Right.tryParse(const {
        'kind': 'subscription',
        'pack': '',
        'origin': 'subscription',
        'expiresAt': '2026-10-19T23:00:00Z',
      });
      expect(one, isNotNull);
      expect(one!.forever, isFalse);
      expect(one.until?.toUtc().month, 10);
    });

    test('бессрочное право узнаётся по пустому сроку', () {
      // Пустая строка, а не отсутствие поля: отсутствие означало бы
      // старый сервер, и показывать это надо иначе.
      final one = Right.tryParse(const {
        'kind': 'pack',
        'pack': 'cardio',
        'origin': 'purchase',
        'expiresAt': '',
      });
      expect(one!.forever, isTrue);
      expect(one.until, isNull);
    });

    test('право с незнакомым родом отбрасывается, а не показывается', () {
      // Словарь закрыт: право неизвестного рода показать нечем, а
      // показанное как подписка сказало бы врачу, что у него есть доступ
      // ко всему корпусу.
      expect(Right.tryParse(const {'kind': 'что-то новое'}), isNull);
    });

    test('право на набор без набора неисполнимо и отбрасывается', () {
      // Показать его значит обещать доступ, которого не выдаст ни одна
      // ручка.
      expect(Right.tryParse(const {'kind': 'pack', 'pack': ''}), isNull);
    });
  });

  group('разбор записи', () {
    test('негодные права считаются, а не пропадают молча', () {
      // Молча выброшенное право выглядит как «у вас его и не было», и
      // объяснить это врачу будет нечем.
      final profile = Profile.parse(const {
        'accountId': 7,
        'email': '',
        'displayName': 'Иванов',
        'createdAt': '2026-09-01T10:00:00Z',
        'rights': [
          {'kind': 'subscription', 'expiresAt': ''},
          {'kind': 'неведомое'},
          {'kind': 'pack', 'pack': ''},
        ],
      });
      expect(profile.rights.length, 1);
      expect(profile.dropped, 2);
      expect(profile.hasSubscription, isTrue);
    });

    test('запись без прав — это пустой список, а не отказ', () {
      // У не заплатившего врача прав нет, и это исправный случай: на
      // свежей установке он единственный.
      final profile = Profile.parse(const {'accountId': 1});
      expect(profile.rights, isEmpty);
      expect(profile.dropped, 0);
      expect(profile.hasSubscription, isFalse);
    });

    test('права, приехавшие не списком, не роняют запись', () {
      // Сборка на руках у врача обязана пережить ответ, которого не
      // ждала: иначе выкатка сервера потребовала бы выкатки приложения в
      // ту же минуту.
      final profile = Profile.parse(const {'accountId': 1, 'rights': 'нет'});
      expect(profile.rights, isEmpty);
      expect(profile.accountId, 1);
    });
  });
}
