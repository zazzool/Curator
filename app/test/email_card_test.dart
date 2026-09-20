/// Проверки привязки почты и возврата доступа.
///
/// Сети здесь нет вовсе: в файле с проверками виджетов Flutter подменяет
/// `HttpClient` и отвечает 400 на всякий запрос. Проверяется разговор с
/// врачом и то, что ушло на сервер, — запись подменена.
///
/// Экран строится узким (320×640) намеренно. По умолчанию проверки
/// виджетов строят 800 точек, где переполнения не бывает, и подтверждают
/// исправность там, где на настоящем телефоне подпись упирается в край.
library;

import 'package:curator/account/account_screen.dart';
import 'package:curator/api/client.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'account_screen_test.dart' show StubAccount, profileOf;

Future<void> openNarrow(WidgetTester tester, StubAccount account) async {
  // Узко, но высоко: ширина — та, на которой подпись упирается в край, а
  // высота нужна, чтобы ListView построил карточку почты. Не построит —
  // и проверка искала бы поле, которого нет, отчитываясь отказом на
  // исправном экране.
  tester.view.physicalSize = const Size(320, 2000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(MaterialApp(home: AccountScreen(account: account)));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('адрес уходит обрезанным, дальше спрашивается код', (
    tester,
  ) async {
    final account = StubAccount(profileOf());
    await openNarrow(tester, account);

    await tester.enterText(find.byKey(const Key('поле имени')).at(0), 'Иванов');
    await tester.enterText(
      find.widgetWithText(TextField, 'Адрес почты'),
      '  VN@Example.COM  ',
    );
    await tester.tap(find.widgetWithText(FilledButton, 'Прислать код'));
    await tester.pumpAndSettle();

    // Набранное уходит как есть: обрезка живёт в Account и проверена там,
    // где живёт (test/api_test.dart). Здесь важно, что экран ничего не
    // потерял по дороге.
    expect(account.bindAsked, '  VN@Example.COM  ');
    // Шаг сменился: спрашивается код, а не адрес снова.
    expect(find.widgetWithText(TextField, 'Код из письма'), findsOneWidget);
    expect(find.textContaining('15 минут'), findsOneWidget);
  });

  testWidgets('подтверждённый код привязывает почту', (tester) async {
    final account = StubAccount(profileOf());
    await openNarrow(tester, account);

    await tester.enterText(
      find.widgetWithText(TextField, 'Адрес почты'),
      'vn@example.com',
    );
    await tester.tap(find.widgetWithText(FilledButton, 'Прислать код'));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.widgetWithText(TextField, 'Код из письма'),
      '123456',
    );
    await tester.tap(find.text('Подтвердить'));
    await tester.pumpAndSettle();

    expect(account.bindConfirmed, '123456');
  });

  testWidgets('отказ сервера показывается словами, а не молчанием', (
    tester,
  ) async {
    final account = StubAccount(profileOf())
      ..bindFailure = ApiFailure('Письмо уже отправлено. Подождите минуту');
    await openNarrow(tester, account);

    await tester.enterText(
      find.widgetWithText(TextField, 'Адрес почты'),
      'vn@example.com',
    );
    await tester.tap(find.widgetWithText(FilledButton, 'Прислать код'));
    await tester.pumpAndSettle();

    expect(
      find.text('Письмо уже отправлено. Подождите минуту'),
      findsOneWidget,
    );
    // Шаг НЕ сменился: врач остался там, где может поправить адрес.
    expect(find.widgetWithText(TextField, 'Адрес почты'), findsOneWidget);
    expect(find.widgetWithText(TextField, 'Код из письма'), findsNothing);
  });

  testWidgets('возврат доступа свёрнут, пока врач его не позвал', (
    tester,
  ) async {
    // На первом запуске он не нужен никому, а развёрнутый выглядел бы
    // обязательным шагом входа — которого здесь нет.
    await openNarrow(tester, StubAccount(profileOf()));
    expect(find.text('Уже пользовались Куратором?'), findsOneWidget);
    expect(
      find.widgetWithText(TextField, 'Почта прежней записи'),
      findsNothing,
    );
  });

  testWidgets('возврат доступа не обещает, что письмо ушло', (tester) async {
    // Ответ сервера один и тот же, знакома ему почта или нет: иначе ручка
    // стала бы способом узнать, кто у нас заведён. Сказать врачу «письмо
    // отправлено» значило бы обещать то, чего мы не знаем.
    final account = StubAccount(profileOf());
    await openNarrow(tester, account);

    await tester.tap(find.text('Уже пользовались Куратором?'));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.widgetWithText(TextField, 'Почта прежней записи'),
      'vn@example.com',
    );
    // Обведённой, а не залитой: подпись на обеих кнопках одна, и «первая
    // попавшаяся» увела бы проверку в другой разговор.
    await tester.tap(find.widgetWithText(OutlinedButton, 'Прислать код'));
    await tester.pumpAndSettle();

    expect(account.recoveryAsked, 'vn@example.com');
    expect(find.textContaining('Если эта почта привязана'), findsOneWidget);
  });

  testWidgets('вернувшийся доступ закрывает экран ответом «да»', (
    tester,
  ) async {
    // Числа на экране позади считаны за прежнюю запись: оставь мы их, врач
    // увидел бы чужой путь под своей вернувшейся почтой.
    final account = StubAccount(profileOf());
    bool? answer;

    // Экран строится своим, а не openNarrow: проверке нужен путь, откуда
    // его открыли, — иначе некуда возвращать ответ. Размер тот же и по
    // той же причине: на низком экране ListView не строит карточку почты.
    tester.view.physicalSize = const Size(320, 2000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => ElevatedButton(
            onPressed: () async {
              answer = await Navigator.of(context).push(
                MaterialPageRoute<bool>(
                  builder: (_) => AccountScreen(account: account),
                ),
              );
            },
            child: const Text('Открыть'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('Открыть'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Уже пользовались Куратором?'));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.widgetWithText(TextField, 'Почта прежней записи'),
      'vn@example.com',
    );
    // Обведённой, а не залитой: подпись на обеих кнопках одна, и «первая
    // попавшаяся» увела бы проверку в другой разговор.
    await tester.tap(find.widgetWithText(OutlinedButton, 'Прислать код'));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.widgetWithText(TextField, 'Код из письма'),
      '654321',
    );
    await tester.tap(find.text('Вернуть доступ'));
    await tester.pumpAndSettle();

    expect(account.recoveryConfirmed, ['vn@example.com', '654321']);
    expect(answer, isTrue);
  });

  testWidgets('поле кода принимает только цифры и ровно шесть', (tester) async {
    final account = StubAccount(profileOf());
    await openNarrow(tester, account);

    await tester.enterText(
      find.widgetWithText(TextField, 'Адрес почты'),
      'vn@example.com',
    );
    await tester.tap(find.widgetWithText(FilledButton, 'Прислать код'));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.widgetWithText(TextField, 'Код из письма'),
      'абв12345678',
    );
    await tester.tap(find.text('Подтвердить'));
    await tester.pumpAndSettle();

    // Буквы отброшены, длина обрезана: код шестизначный, и отправлять
    // заведомо негодное значит тратить попытку врача.
    expect(account.bindConfirmed, '123456');
  });
}
