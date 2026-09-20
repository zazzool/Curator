/// Проверки сборки, собранной без ключей.
///
/// Оба ключа задаются при сборке и имеют умолчанием пустую строку. Такая
/// сборка выглядела исправной и не могла ничего: устройство не
/// заводилось, а на всякий набор шёл ответ «Этот набор подписан ключом,
/// которого нет в приложении. Обновите приложение» — врачу велят обновить
/// то, что свежее некуда.
library;

import 'package:curator/main.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('недостающие ключи называются поимённо', () {
    expect(missingDefines('app-key-1', 'key-1:AAAA'), isEmpty);

    expect(missingDefines('', 'key-1:AAAA'), ['CURATOR_APP_KEY']);
    expect(missingDefines('app-key-1', ''), ['CURATOR_PACK_KEYS']);
    expect(missingDefines('', ''), ['CURATOR_APP_KEY', 'CURATOR_PACK_KEYS']);
  });

  test('строка ключей из одного мусора считается пустой', () {
    // Разбор ключей отбрасывает негодную запись молча — одна опечатка в
    // строке сборки не должна лишать приложение всех ключей сразу. Но
    // строка, из которой не разобралось НИ ОДНОГО ключа, — это та же
    // сборка без ключей, и признать её годной значит вернуть тот же
    // дефект через опечатку.
    expect(missingDefines('app-key-1', 'безДвоеточия'), ['CURATOR_PACK_KEYS']);
    expect(missingDefines('app-key-1', ':'), ['CURATOR_PACK_KEYS']);
  });

  testWidgets('сборка без ключей называет, чего ей не хватает', (tester) async {
    // Экран, а не исключение: упавшее при подъёме приложение не говорит
    // ничего — ни белым экраном на устройстве, ни строкой в журнале, до
    // которой ещё надо догадаться дойти.
    await tester.pumpWidget(
      const MisbuiltApp(missing: ['CURATOR_APP_KEY', 'CURATOR_PACK_KEYS']),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('CURATOR_APP_KEY'), findsOneWidget);
    expect(find.textContaining('CURATOR_PACK_KEYS'), findsOneWidget);
  });
}
