import 'package:curator/api/client.dart';
import 'package:curator/main.dart';
import 'package:flutter_test/flutter_test.dart';

/// Первый экран показывает, чем кончилось заведение устройства.
///
/// Экран, молча остающийся пустым при отказе, врач читает как поломку
/// приложения и идёт его переустанавливать — то есть делает ровно то, что
/// не поможет.
class _FailingStore implements TokenStore {
  @override
  Future<String?> read() async => null;

  @override
  Future<void> write(String token) async {}

  @override
  Future<void> clear() async {}
}

void main() {
  testWidgets('отказ показывается словами и повторяется по кнопке', (
    tester,
  ) async {
    // В проверках виджетов Flutter подменяет HttpClient своим, и тот
    // отвечает отказом на всё. Нам это и нужно: важно не то, какой отказ
    // случился, а то, что он доехал до экрана словами и с кнопкой, а не
    // оставил экран пустым.
    final api = Api(
      baseUrl: Uri.parse('http://127.0.0.1:1'),
      appKey: 'app-key-1',
      tokens: _FailingStore(),
    );
    addTearDown(api.close);

    await tester.pumpWidget(CuratorApp(api: api));
    await tester.pumpAndSettle();

    expect(find.text('Ещё раз'), findsOneWidget);
    expect(find.textContaining('Задачи появятся'), findsNothing);

    await tester.tap(find.text('Ещё раз'));
    await tester.pumpAndSettle();
    // Повтор не роняет экран и оставляет его говорящим.
    expect(find.text('Ещё раз'), findsOneWidget);
  });
}
