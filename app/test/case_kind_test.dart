/// Словарь видов задачи сверяется с общим эталоном.
///
/// Вид задачи стороны читают по-разному, и в этом вся соль: сервер решает
/// им, чем сверять ответ — меткой варианта или его текстом, — а
/// приложение решает, понятна ли задача вообще. Пока общей записи не
/// было, стороны разошлись: сервер считал годной задачу-узнавание с
/// вариантом без метки и публиковал её, приложение не понимало её ничем
/// и выбрасывало целиком. Составитель видел задачу изданной, врач не
/// видел её вовсе.
///
/// Поэтому список видов лежит в `shared/wire-contract.json`, и сверяются
/// с ним обе стороны: здесь и в `server/internal/app/contract_test.go`.
library;

import 'package:flutter_test/flutter_test.dart';

import 'package:curator/cases/model.dart';

import 'shared_reference.dart';

void main() {
  test('словарь видов задачи совпадает с эталоном', () {
    final contract = readShared('wire-contract.json');
    final body = contract['caseBody'] as Map<String, dynamic>;
    final written = (body['kinds'] as List?)?.cast<String>();

    expect(
      written,
      isNotNull,
      reason:
          'в эталоне не записан словарь видов (caseBody.kinds): без него '
          'стороны снова разойдутся молча',
    );

    final ours = CaseKind.values.map((one) => one.name).toList()..sort();
    final theirs = [...written!]..sort();
    expect(
      ours,
      theirs,
      reason:
          'приложение знает виды $ours, а эталон — $theirs. Вид, известный '
          'только одной стороне, — это задача, которая у другой пропадает',
    );
  });

  test('у узнавания ответ сверяется с меткой, у действия — с текстом', () {
    // Здесь то самое правило, на котором стороны разошлись. Сервер
    // спрашивает его у casestore.Body.CorrectOption, приложение — у
    // chosenValue, и обе записи обязаны говорить одно.
    final recognise = CaseItem.tryParse({
      'id': 'c-1',
      'body': {
        'kind': 'recognise',
        'title': 'Узнавание',
        'answer': 'F20.0',
        'segments': [
          {
            'text': 'Условие',
            'statements': ['абз. 1'],
          },
        ],
        'options': [
          {'label': 'F20.0', 'text': 'Параноидная шизофрения'},
          {'label': 'F20.1', 'text': 'Гебефреническая шизофрения'},
        ],
        'explanationMd': 'Разбор',
        'difficulty': 3,
      },
    });
    expect(recognise, isNotNull);
    expect(recognise!.chosenValue(recognise.options.first), 'F20.0');
    expect(recognise.isCorrect(recognise.options.first), isTrue);

    // Узнавание с вариантом без метки — ровно тот случай, который сервер
    // прежде публиковал. Приложение его не понимает, и это не придирка:
    // сверять ответ здесь не с чем.
    final unlabelled = CaseItem.tryParse({
      'id': 'c-2',
      'body': {
        'kind': 'recognise',
        'title': 'Узнавание без меток',
        'answer': 'Параноидная шизофрения',
        'segments': [
          {
            'text': 'Условие',
            'statements': ['абз. 1'],
          },
        ],
        'options': [
          {'text': 'Параноидная шизофрения'},
          {'text': 'Гебефреническая шизофрения'},
        ],
        'explanationMd': 'Разбор',
        'difficulty': 3,
      },
    });
    expect(
      unlabelled,
      isNull,
      reason:
          'у узнавания ответ сверяется с меткой варианта: без меток '
          'сверять нечем, и задача разбирается в null целиком',
    );

    final action = CaseItem.tryParse({
      'id': 'c-3',
      'body': {
        'kind': 'action',
        'title': 'Действие',
        'answer': 'Назначить осмотр',
        'segments': [
          {
            'text': 'Условие',
            'statements': ['абз. 2'],
          },
        ],
        'options': [
          {'text': 'Назначить осмотр'},
          {'text': 'Отложить осмотр'},
        ],
        'explanationMd': 'Разбор',
        'difficulty': 3,
      },
    });
    expect(action, isNotNull);
    expect(action!.chosenValue(action.options.first), 'Назначить осмотр');
    expect(action.isCorrect(action.options.first), isTrue);
  });
}
