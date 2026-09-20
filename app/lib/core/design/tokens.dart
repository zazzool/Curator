import 'package:flutter/widgets.dart';

/// Дизайн-токены: единый ритм отступов, радиусов и длительностей.
///
/// Правило: в UI не должно быть «магических» чисел — только эти константы.
/// Шаг сетки — 4 pt.
abstract final class Gap {
  static const double xs = 2;
  static const double sm = 6;
  static const double md = 10;
  static const double lg = 14;
  static const double xl = 20;
  static const double xxl = 28;
  static const double huge = 40;

  /// Горизонтальные поля экрана. Одна величина на всё приложение: контент
  /// выстраивается по общей левой линии, вложенные отступы не добавляются.
  static const EdgeInsets screenH = EdgeInsets.symmetric(horizontal: 16);
}

/// Скругления. Плоскости и элементы управления скруглены сдержанно, а
/// «игровые» блоки прогресса — заметно сильнее: крупный радиус читается как
/// дружелюбный жест и отделяет геймификацию от справочной части.
abstract final class Radii {
  static const Radius control = Radius.circular(8);
  static const Radius surface = Radius.circular(10);

  /// Крупные карточки прогресса, ачивки, hero-блоки.
  static const Radius hero = Radius.circular(20);

  /// Полностью скруглённые элементы: полосы XP, счётчики, теги.
  static const Radius pill = Radius.circular(999);

  /// Полноширинная зона во всю ширину экрана (тёмная шапка «Прогресса»):
  /// крупнее hero-карточек, потому что скругляет не карточку, а «мир».
  static const Radius zone = Radius.circular(28);

  static const BorderRadius controlAll = BorderRadius.all(control);
  static const BorderRadius surfaceAll = BorderRadius.all(surface);
  static const BorderRadius heroAll = BorderRadius.all(hero);
  static const BorderRadius pillAll = BorderRadius.all(pill);
}

abstract final class Motion {
  static const Duration fast = Duration(milliseconds: 160);
  static const Duration base = Duration(milliseconds: 260);
  static const Duration slow = Duration(milliseconds: 620);

  /// Отыгровка достижения: набег счётчиков, залп конфетти.
  static const Duration celebrate = Duration(milliseconds: 1400);

  /// Полный цикл блика, бегущего по активной полосе прогресса.
  static const Duration shimmer = Duration(milliseconds: 2200);

  static const Curve enter = Curves.easeOutCubic;
  static const Curve standard = Curves.easeInOutCubicEmphasized;

  /// Пружина с лёгким перелётом — появление счётчиков и бейджей.
  static const Curve spring = Curves.easeOutBack;

  /// Шаг задержки в каскадном появлении списка.
  ///
  /// Каскад должен читаться как одно движение сверху вниз, а не как
  /// поочерёдная загрузка блоков: на холодном старте он складывается с
  /// ожиданием базы, и длинная волна превращается в пустой экран.
  static const Duration stagger = Duration(milliseconds: 28);

  /// Сколько шагов каскада вообще успевают отличаться друг от друга.
  /// Дальше задержка не растёт — иначе низ длинного экрана ждал бы
  /// заметно дольше верха без всякой пользы.
  static const int staggerCap = 6;
}
