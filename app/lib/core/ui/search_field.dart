import 'package:flutter/material.dart';

import '../design/palette.dart';
import '../design/tokens.dart';
import '../design/typography.dart';
import 'surface.dart';

/// Поле поиска по источнику.
///
/// Это не Material `SearchBar`: у того семантический узел ограничен строкой
/// текста (~21 pt), из-за чего цель нажатия для скринридера и Switch Access
/// оказывается меньше рекомендованных 48 pt. Здесь высоту задаёт
/// `contentPadding` самого поля, поэтому семантический узел совпадает с
/// видимой областью.
class AppSearchField extends StatelessWidget {
  const AppSearchField({
    super.key,
    required this.hintText,
    required this.onChanged,
    this.controller,
    this.autofocus = false,
    this.semanticLabel,
    this.onClear,
  });

  final String hintText;
  final ValueChanged<String> onChanged;
  final TextEditingController? controller;
  final bool autofocus;
  final String? semanticLabel;

  /// Сброс запроса. Задаётся там, где набранное надо уметь стереть одним
  /// нажатием: очистить поле выделением и забоем на телефоне — это четыре
  /// точных касания вместо одного.
  final VoidCallback? onClear;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return TextField(
      controller: controller,
      autofocus: autofocus,
      onChanged: onChanged,
      textInputAction: TextInputAction.search,
      style: AppType.body.copyWith(color: p.ink),
      cursorColor: p.accent,
      decoration: InputDecoration(
        hintText: hintText,
        hintStyle: AppType.body.copyWith(color: p.inkFaint),
        // Вертикальные отступы держат высоту поля на уровне 56 pt.
        contentPadding: const EdgeInsets.symmetric(vertical: 18),
        prefixIcon: Icon(Icons.search, size: 20, color: p.inkFaint),
        prefixIconConstraints: const BoxConstraints(
          minWidth: minTouchTarget,
          minHeight: minTouchTarget,
        ),
        suffixIcon: onClear == null
            ? null
            : IconButton(
                icon: Icon(Icons.close, size: 18, color: p.inkFaint),
                tooltip: 'Очистить',
                onPressed: onClear,
              ),
        filled: true,
        fillColor: p.surface,
        isDense: false,
        border: _border(p.hairline),
        enabledBorder: _border(p.hairline),
        focusedBorder: _border(p.accent),
        labelStyle: AppType.body,
      ),
    );
  }

  OutlineInputBorder _border(Color color) => OutlineInputBorder(
    borderRadius: Radii.controlAll,
    borderSide: BorderSide(color: color),
  );
}
