/// Почта: привязка адреса и возврат доступа.
///
/// # Зачем это врачу
///
/// Запись заводится молча при первом запуске и живёт на этом устройстве.
/// Со сменой телефона врач теряет всё: разобранные задачи, знаки,
/// купленные наборы. Почта — единственный способ это вернуть, и других
/// доводов у записи нет: пароля в продукте не существует.
///
/// # Почему отдельным файлом
///
/// Здесь два разных разговора с врачом, у каждого по два шага и своё
/// состояние. В экране доступа они заняли бы больше места, чем всё
/// остальное на нём вместе взятое, и правка одного задевала бы другой.
library;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api/client.dart';
import 'account.dart';

class EmailCard extends StatefulWidget {
  const EmailCard({
    super.key,
    required this.account,
    required this.email,
    required this.onBound,
    required this.onRecovered,
  });

  final Account account;

  /// Привязанный адрес. Пусто — почты нет.
  final String email;

  /// Позвать, когда адрес привязан: экран перечитывает запись.
  final void Function(String email) onBound;

  /// Позвать, когда доступ вернулся: токен сменился, и перечитать надо
  /// не только этот экран.
  final VoidCallback onRecovered;

  @override
  State<EmailCard> createState() => _EmailCardState();
}

class _EmailCardState extends State<EmailCard> {
  final _bindEmail = TextEditingController();
  final _bindCode = TextEditingController();
  final _backEmail = TextEditingController();
  final _backCode = TextEditingController();

  /// Код запрошен: показан шаг ввода кода, а не шаг ввода адреса.
  bool _bindAsked = false;
  bool _backAsked = false;

  /// Врач сказал, что уже пользовался Куратором. Свёрнуто по умолчанию:
  /// на первом запуске возврат доступа не нужен никому, а развёрнутый он
  /// выглядел бы как обязательный шаг входа — которого здесь нет.
  bool _backOpen = false;

  bool _busy = false;

  @override
  void dispose() {
    _bindEmail.dispose();
    _bindCode.dispose();
    _backEmail.dispose();
    _backCode.dispose();
    super.dispose();
  }

  void _say(String text) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
  }

  /// Выполняет шаг, не давая нажать дважды.
  ///
  /// Второе нажатие по «Прислать код» стоит врачу минуты ожидания: между
  /// двумя письмами на один адрес сервер выдерживает паузу, и второе
  /// обращение вернулось бы отказом на исправное действие.
  Future<void> _step(Future<void> Function() body) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await body();
    } on ApiFailure catch (failure) {
      _say(failure.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Почта', style: theme.textTheme.titleMedium),
        const SizedBox(height: 8),
        if (widget.email.isNotEmpty)
          _Bound(email: widget.email)
        else ...[
          _bindBlock(theme),
          const SizedBox(height: 16),
          _backBlock(theme),
        ],
      ],
    );
  }

  // --- Привязка ---

  Widget _bindBlock(ThemeData theme) {
    if (!_bindAsked) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'Запись живёт на этом устройстве. Привяжите почту — и при '
            'смене телефона вернёте задачи, знаки и купленный доступ.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _bindEmail,
            keyboardType: TextInputType.emailAddress,
            autocorrect: false,
            decoration: const InputDecoration(
              labelText: 'Адрес почты',
              prefixIcon: Icon(Icons.alternate_email),
              border: OutlineInputBorder(),
            ),
            onSubmitted: (_) => _askBind(),
          ),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerRight,
            child: FilledButton(
              onPressed: _busy ? null : _askBind,
              child: const Text('Прислать код'),
            ),
          ),
        ],
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'Код отправлен на ${_bindEmail.text.trim()}. Он годен 15 минут. '
          'Если письма нет — загляните в «Спам».',
          style: theme.textTheme.bodyMedium,
        ),
        const SizedBox(height: 12),
        _CodeField(controller: _bindCode, onSubmitted: _confirmBind),
        const SizedBox(height: 8),
        // Wrap, а не Row: на узком экране две подписи не умещаются в
        // строку, и Row упёрся бы в край, спрятав главную кнопку. Проверка
        // на 320 точках поймала это на первом же прогоне.
        Wrap(
          alignment: WrapAlignment.end,
          spacing: 8,
          runSpacing: 4,
          children: [
            TextButton(
              // Не «прислать ещё раз»: чаще всего человек ошибся в адресе,
              // и повторное письмо ушло бы туда же.
              onPressed: _busy
                  ? null
                  : () => setState(() => _bindAsked = false),
              child: const Text('Другой адрес'),
            ),
            FilledButton(
              onPressed: _busy ? null : _confirmBind,
              child: const Text('Подтвердить'),
            ),
          ],
        ),
      ],
    );
  }

  Future<void> _askBind() => _step(() async {
    await widget.account.startBind(_bindEmail.text);
    if (!mounted) return;
    setState(() => _bindAsked = true);
  });

  Future<void> _confirmBind() => _step(() async {
    final bound = await widget.account.confirmBind(_bindCode.text);
    if (!mounted) return;
    _bindCode.clear();
    _say('Почта привязана');
    widget.onBound(bound);
  });

  // --- Возврат доступа ---

  Widget _backBlock(ThemeData theme) {
    if (!_backOpen) {
      return TextButton.icon(
        onPressed: () => setState(() => _backOpen = true),
        icon: const Icon(Icons.restore),
        label: const Text('Уже пользовались Куратором?'),
      );
    }

    if (!_backAsked) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'Назовите почту, привязанную к прежней записи. Задачи, знаки '
            'и купленный доступ вернутся на это устройство.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _backEmail,
            keyboardType: TextInputType.emailAddress,
            autocorrect: false,
            decoration: const InputDecoration(
              labelText: 'Почта прежней записи',
              prefixIcon: Icon(Icons.restore),
              border: OutlineInputBorder(),
            ),
            onSubmitted: (_) => _askBack(),
          ),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerRight,
            child: OutlinedButton(
              onPressed: _busy ? null : _askBack,
              child: const Text('Прислать код'),
            ),
          ),
        ],
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Ответ сервера один и тот же, знакома ему эта почта или нет, —
        // иначе ручка стала бы способом узнать, кто у нас заведён.
        // Поэтому и здесь сказано «если привязана», а не «отправлено».
        Text(
          'Если эта почта привязана к записи, письмо с кодом уже '
          'отправлено. Код годен 15 минут.',
          style: theme.textTheme.bodyMedium,
        ),
        const SizedBox(height: 12),
        _CodeField(controller: _backCode, onSubmitted: _confirmBack),
        const SizedBox(height: 8),
        Wrap(
          alignment: WrapAlignment.end,
          spacing: 8,
          runSpacing: 4,
          children: [
            TextButton(
              onPressed: _busy
                  ? null
                  : () => setState(() => _backAsked = false),
              child: const Text('Другой адрес'),
            ),
            FilledButton(
              onPressed: _busy ? null : _confirmBack,
              child: const Text('Вернуть доступ'),
            ),
          ],
        ),
      ],
    );
  }

  Future<void> _askBack() => _step(() async {
    await widget.account.startRecovery(_backEmail.text);
    if (!mounted) return;
    setState(() => _backAsked = true);
  });

  Future<void> _confirmBack() => _step(() async {
    await widget.account.confirmRecovery(_backEmail.text, _backCode.text);
    if (!mounted) return;
    _backCode.clear();
    _say('Доступ вернулся');
    widget.onRecovered();
  });
}

/// Привязанный адрес.
class _Bound extends StatelessWidget {
  const _Bound({required this.email});

  final String email;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        // Волосяная граница, а не тень: тень допустима только у того, что
        // физически висит над страницей, а это часть страницы.
        border: Border.all(color: theme.dividerColor),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.mark_email_read_outlined,
            color: theme.colorScheme.primary,
          ),
          const SizedBox(width: 12),
          // Гибким, а не как есть: длинный адрес на узком экране упирался
          // бы в край вместо переноса.
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(email, style: theme.textTheme.bodyLarge),
                const SizedBox(height: 4),
                Text(
                  'Смените телефон — доступ вернётся по этой почте.',
                  style: theme.textTheme.bodySmall,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Поле шестизначного кода.
///
/// Отдельным виджетом, потому что набирается он дважды и одинаково: цифры,
/// ровно шесть, с цифровой клавиатурой. Разойдись эти два поля — и одно из
/// них однажды начало бы принимать буквы.
class _CodeField extends StatelessWidget {
  const _CodeField({required this.controller, required this.onSubmitted});

  final TextEditingController controller;
  final VoidCallback onSubmitted;

  @override
  Widget build(BuildContext context) {
    return TextField(
      controller: controller,
      keyboardType: TextInputType.number,
      maxLength: 6,
      inputFormatters: [FilteringTextInputFormatter.digitsOnly],
      decoration: const InputDecoration(
        labelText: 'Код из письма',
        prefixIcon: Icon(Icons.pin_outlined),
        border: OutlineInputBorder(),
        counterText: '',
      ),
      onSubmitted: (_) => onSubmitted(),
    );
  }
}
