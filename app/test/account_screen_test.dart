/// Проверки экрана «Мой доступ».
///
/// Сети здесь нет вовсе, и не потому, что её лень поднимать: в файле с
/// проверками виджетов Flutter подменяет `HttpClient` и отвечает 400 на
/// всякий запрос. Запись приходит на экран готовой — её и подменяем.
library;

import 'package:curator/account/account.dart';
import 'package:curator/account/account_screen.dart';
import 'package:curator/account/model.dart';
import 'package:curator/api/client.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Запись, отвечающая заданным.
class StubAccount implements Account {
  StubAccount(this.profile, {this.failure});

  final Profile profile;
  final ApiFailure? failure;
  String? renamedTo;

  @override
  Future<Profile> read() async {
    final one = failure;
    if (one != null) throw one;
    return profile;
  }

  @override
  Future<void> rename(String name) async {
    renamedTo = name;
  }

  /// Что попросили и чем ответили. Записывается ДОСЛОВНО: обрезка пробелов
  /// живёт в Account, и заглушка, обрезающая сама, проверяла бы себя.
  /// Обрезка проверена в test/api_test.dart против настоящего сокета.
  String? bindAsked;
  String? bindConfirmed;
  String? recoveryAsked;
  List<String>? recoveryConfirmed;

  /// Чем отвечать. Отказ здесь — это отказ сервера, и экран обязан
  /// показать его словами, а не промолчать.
  ApiFailure? bindFailure;
  ApiFailure? recoveryFailure;

  @override
  Future<void> startBind(String email) async {
    final one = bindFailure;
    if (one != null) throw one;
    bindAsked = email;
  }

  @override
  Future<String> confirmBind(String code) async {
    final one = bindFailure;
    if (one != null) throw one;
    bindConfirmed = code;
    return bindAsked ?? '';
  }

  @override
  Future<void> startRecovery(String email) async {
    final one = recoveryFailure;
    if (one != null) throw one;
    recoveryAsked = email;
  }

  @override
  Future<void> confirmRecovery(String email, String code) async {
    final one = recoveryFailure;
    if (one != null) throw one;
    recoveryConfirmed = [email, code];
  }
}

Profile profileOf({
  List<Right> rights = const [],
  int dropped = 0,
  String email = '',
}) => Profile(
  accountId: 7,
  email: email,
  displayName: 'Иванов',
  createdAt: '2026-09-01T10:00:00Z',
  rights: rights,
  dropped: dropped,
);

Future<void> openScreen(WidgetTester tester, StubAccount account) async {
  await tester.pumpWidget(MaterialApp(home: AccountScreen(account: account)));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('подписка названа сроком, а не просто наличием', (tester) async {
    // «Подписка есть» не отвечает на вопрос, ради которого врач сюда и
    // зашёл: до какого числа она есть.
    await openScreen(
      tester,
      StubAccount(
        profileOf(
          rights: const [
            Right(
              kind: 'subscription',
              pack: '',
              origin: 'subscription',
              expiresAt: '2026-10-19T10:00:00Z',
            ),
          ],
        ),
      ),
    );
    expect(find.text('Подписка на весь корпус'), findsOneWidget);
    expect(find.textContaining('19 октября 2026'), findsOneWidget);
  });

  testWidgets('бессрочное право названо бессрочным', (tester) async {
    await openScreen(
      tester,
      StubAccount(
        profileOf(
          rights: const [
            Right(
              kind: 'pack',
              pack: 'cardio',
              origin: 'purchase',
              expiresAt: '',
            ),
          ],
        ),
      ),
    );
    expect(find.text('Набор «cardio»'), findsOneWidget);
    expect(find.text('Бессрочно'), findsOneWidget);
  });

  testWidgets('без прав сказано, что скачанное остаётся', (tester) async {
    // Врач без подписки не должен думать, что у него отберут скачанные
    // наборы: их не отбирают, и сказать это надо здесь.
    await openScreen(tester, StubAccount(profileOf()));
    expect(find.textContaining('Платного доступа сейчас нет'), findsOneWidget);
    expect(find.textContaining('их не отбирают'), findsOneWidget);
  });

  testWidgets('непоказанные права названы числом', (tester) async {
    // Молча выброшенное право выглядит как «у вас его и не было».
    await openScreen(tester, StubAccount(profileOf(dropped: 2)));
    expect(find.textContaining('показать не умеет: 2'), findsOneWidget);
  });

  testWidgets('непривязанная почта зовёт привязать, а не обещает', (
    tester,
  ) async {
    // Обещание «позже» отправило бы врача ждать. Здесь привязка есть, и
    // сказано именно это — вместе с тем, ради чего она нужна.
    await openScreen(tester, StubAccount(profileOf()));
    expect(find.text('Прислать код'), findsOneWidget);
    final why = tester.widget<Text>(find.textContaining('при смене телефона'));
    expect(why.data, isNot(contains('позже')));
    expect(why.data, isNot(contains('скоро')));
  });

  testWidgets('привязанная почта показана вместе с тем, зачем она', (
    tester,
  ) async {
    await openScreen(tester, StubAccount(profileOf(email: 'vn@example.com')));
    expect(find.text('vn@example.com'), findsOneWidget);
    // Поля ввода нет вовсе: привязанное не переспрашивают.
    expect(find.text('Прислать код'), findsNothing);
  });

  testWidgets('набранное имя уходит на сохранение как есть', (tester) async {
    // Экран отдаёт набранное, не мудря: обрезка — дело Account, и проверена
    // она там, где живёт (test/api_test.dart).
    final account = StubAccount(profileOf());
    await openScreen(tester, account);
    await tester.enterText(
      find.byKey(const Key('поле имени')),
      '  Пётр Петрович  ',
    );
    await tester.tap(find.text('Сохранить'));
    await tester.pumpAndSettle();
    expect(account.renamedTo, '  Пётр Петрович  ');
  });

  testWidgets('отказ показывается словами сервера и даёт повторить', (
    tester,
  ) async {
    await openScreen(
      tester,
      StubAccount(
        profileOf(),
        failure: ApiFailure('Сеть недоступна. Попробуйте позже'),
      ),
    );
    expect(find.text('Сеть недоступна. Попробуйте позже'), findsOneWidget);
    expect(find.text('Ещё раз'), findsOneWidget);
  });
}
