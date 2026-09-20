/// Выход с экрана, который отказал по отозванному токену.
///
/// Отдельным виджетом, а не строкой в каждом экране: мест, где врач
/// впервые читает «устройство не опознано», больше одного, и путь отсюда
/// обязан быть один и тот же. Разойдись он по экранам, где-нибудь он
/// просто пропал бы — а пропав, оставил бы исправное приложение, в
/// которое нельзя войти.
library;

import 'package:flutter/material.dart';

import '../api/client.dart';
import 'account.dart';
import 'account_screen.dart';

class RecoverAccessButton extends StatelessWidget {
  const RecoverAccessButton({
    super.key,
    required this.api,
    required this.onRecovered,
  });

  final Api api;

  /// Позвать, когда доступ вернулся: токен сменился, и всё, что экран
  /// показывает, относится к прежней записи.
  final VoidCallback onRecovered;

  @override
  Widget build(BuildContext context) {
    return FilledButton.icon(
      onPressed: () => _open(context),
      icon: const Icon(Icons.restore),
      label: const Text('Вернуть доступ'),
    );
  }

  Future<void> _open(BuildContext context) async {
    // Навигатор берётся ДО ожидания: после него `context` может уже не
    // принадлежать дереву, и правило Flutter про это не придирка — экран,
    // закрытый врачом в эти секунды, уронил бы приложение.
    final navigator = Navigator.of(context);
    final back = await navigator.push(
      MaterialPageRoute<bool>(
        builder: (_) => AccountScreen(account: Account(api)),
      ),
    );
    if (back == true) onRecovered();
  }
}
