/// Задача, какой её видит приложение.
///
/// # Непонятое не применяется
///
/// Задача, разобранная наполовину, хуже отсутствующей: варианты без
/// условия или условие без верного ответа выглядят на экране рабочими, и
/// врач отвечает на то, чего не показали. Поэтому разбор либо отдаёт
/// задачу целиком, либо не отдаёт ничего — а пачка при этом не роняется:
/// одна битая задача не должна стоить врачу всей ленты.
///
/// # Лишнее поле — не отказ
///
/// Сервер может дослать поле, которого эта сборка не знает: формат растёт
/// добавлением, и на руках у врачей стоят сборки, которые обновятся не
/// завтра. Незнакомое просто не читается.
library;

/// Вид задачи. Словарь закрыт — тем же, что и на сервере.
enum CaseKind {
  /// Узнай единицу по описанию.
  recognise,

  /// Выбери верное действие.
  action;

  static CaseKind? parse(String raw) {
    for (final one in CaseKind.values) {
      if (one.name == raw) return one;
    }
    return null;
  }

  /// Вид словами — их читает врач, а не программист.
  String get word => switch (this) {
    CaseKind.recognise => 'Узнавание',
    CaseKind.action => 'Выбор действия',
  };
}

/// Кусок условия.
class Segment {
  const Segment({required this.text, this.statements = const []});

  final String text;

  /// Обозначения положений («абз. 1»), которые этот кусок подтверждает.
  /// Обозначение, а не номер: номер осмыслен только внутри нашей базы, а
  /// обозначение — то, что врач найдёт в первоисточнике.
  final List<String> statements;
}

/// Вариант ответа.
class Option {
  const Option({required this.text, this.label = ''});

  final String text;

  /// Метка варианта у задачи-узнавания. У варианта-действия метки нет, и
  /// пустая строка здесь — исправный случай, а не потеря.
  final String label;
}

/// Задача целиком.
class CaseItem {
  const CaseItem({
    required this.id,
    required this.title,
    required this.kind,
    required this.segments,
    required this.options,
    required this.answer,
    required this.explanationMd,
    this.difficulty = 0,
    this.unitLabel = '',
    this.sourceId = 0,
  });

  final String id;
  final String title;
  final CaseKind kind;
  final List<Segment> segments;
  final List<Option> options;

  /// Метка верного варианта у задачи-узнавания, текст действия у
  /// задачи-действия.
  final String answer;

  final String explanationMd;
  final int difficulty;
  final String unitLabel;
  final int sourceId;

  /// Верен ли выбранный вариант.
  ///
  /// Сверяется то же, что уедет на сервер в попытке: у узнавания — метка,
  /// у действия — текст. Сверь мы здесь одно, а пошли другое — и
  /// решаемость задачи считалась бы не по тому, что видел врач.
  bool isCorrect(Option chosen) => chosenValue(chosen) == answer;

  /// Что именно уходит на сервер как выбранный ответ.
  String chosenValue(Option chosen) =>
      kind == CaseKind.recognise ? chosen.label : chosen.text;

  /// Разбор одной задачи ленты.
  ///
  /// Возвращает `null` на непонятом — целиком, не подменяя недостающее
  /// умолчаниями. Задача без вариантов или без верного ответа — это
  /// задача, на которую нельзя ответить, и показать её значит соврать.
  static CaseItem? tryParse(Map<String, dynamic> row) {
    final id = row['id'];
    final body = row['body'];
    if (id is! String || id.isEmpty || body is! Map<String, dynamic>) {
      return null;
    }
    final kind = CaseKind.parse(_string(body['kind']));
    final answer = _string(body['answer']);
    if (kind == null || answer.isEmpty) return null;

    final segments = _list(body['segments'])
        .map(
          (one) => Segment(
            text: _string(one['text']),
            statements: _list(
              one['statements'],
              objects: false,
            ).map(_string).where((s) => s.isNotEmpty).toList(),
          ),
        )
        .where((one) => one.text.isNotEmpty)
        .toList();

    final options = _list(body['options'])
        .map(
          (one) =>
              Option(text: _string(one['text']), label: _string(one['label'])),
        )
        .where((one) => one.text.isNotEmpty)
        .toList();

    if (segments.isEmpty || options.length < 2) {
      // Условие без текста нечего читать, а один вариант — не выбор.
      return null;
    }

    final item = CaseItem(
      id: id,
      title: _string(body['title']),
      kind: kind,
      segments: segments,
      options: options,
      answer: answer,
      explanationMd: _string(body['explanationMd']),
      difficulty: _int(body['difficulty']),
      unitLabel: _string(row['unitLabel']),
      sourceId: _int(row['sourceId']),
    );

    // Верный ответ обязан быть среди вариантов. Задача, у которой его нет,
    // не решается никем и никогда — и это не сложность, а поломка.
    if (!options.any((one) => item.chosenValue(one) == answer)) return null;
    return item;
  }
}

String _string(Object? value) => value is String ? value : '';

int _int(Object? value) => switch (value) {
  final int v => v,
  final double v => v.round(),
  _ => 0,
};

List<dynamic> _list(Object? value, {bool objects = true}) {
  if (value is! List) return const [];
  if (!objects) return value;
  return value.whereType<Map<String, dynamic>>().toList();
}
