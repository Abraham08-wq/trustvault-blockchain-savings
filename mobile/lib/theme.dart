import 'package:flutter/material.dart';

/// ChamaChain brand colours, shared with the Shield console.
class Brand {
  static const abyss = Color(0xFF050C1C);
  static const deep = Color(0xFF0A1734);
  static const panel = Color(0xFF0E1F45);
  static const rule = Color(0xFF1C3566);
  static const text = Color(0xFFDCE6FF);
  static const muted = Color(0xFF8396C0);
  static const signal = Color(0xFF4CC9FF);
  static const safe = Color(0xFF3BE8A8);
  static const alert = Color(0xFFFF5C78);
  static const caution = Color(0xFFFFB443);

  static Color risk(String level) {
    switch (level) {
      case 'high':
        return alert;
      case 'medium':
        return caution;
      default:
        return signal;
    }
  }
}

ThemeData buildTheme() {
  const scheme = ColorScheme.dark(
    primary: Brand.signal,
    onPrimary: Color(0xFF021022),
    secondary: Brand.safe,
    onSecondary: Color(0xFF02150E),
    error: Brand.alert,
    surface: Brand.abyss,
    onSurface: Brand.text,
  );
  final base = ThemeData(useMaterial3: true, colorScheme: scheme, scaffoldBackgroundColor: Brand.abyss);
  return base.copyWith(
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size(0, 52),
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(0, 52),
        foregroundColor: Brand.text,
        side: const BorderSide(color: Brand.rule),
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w500),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    snackBarTheme: const SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: Brand.panel,
      contentTextStyle: TextStyle(color: Brand.text),
    ),
    dividerTheme: const DividerThemeData(color: Brand.rule, space: 1),
  );
}

/// Text field decoration used across the app.
InputDecoration field(String label, {String? hint, String? prefix, String? helper}) {
  OutlineInputBorder border(Color c, [double w = 1]) =>
      OutlineInputBorder(borderRadius: BorderRadius.circular(10), borderSide: BorderSide(color: c, width: w));
  return InputDecoration(
    labelText: label,
    hintText: hint,
    prefixText: prefix,
    helperText: helper,
    helperMaxLines: 3,
    filled: true,
    fillColor: Brand.abyss,
    labelStyle: const TextStyle(color: Brand.muted),
    border: border(Brand.rule),
    enabledBorder: border(Brand.rule),
    focusedBorder: border(Brand.signal, 1.6),
    errorBorder: border(Brand.alert),
    focusedErrorBorder: border(Brand.alert, 1.6),
  );
}

/// A bordered panel, the app's basic surface.
class Panel extends StatelessWidget {
  const Panel({super.key, required this.child, this.padding = const EdgeInsets.all(18), this.accent, this.onTap});

  final Widget child;
  final EdgeInsets padding;
  final Color? accent;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final box = Container(
      padding: padding,
      decoration: BoxDecoration(
        color: Brand.panel,
        borderRadius: BorderRadius.circular(14),
        border: Border(
          left: BorderSide(color: accent ?? Brand.rule, width: accent == null ? 1 : 3),
          top: const BorderSide(color: Brand.rule),
          right: const BorderSide(color: Brand.rule),
          bottom: const BorderSide(color: Brand.rule),
        ),
      ),
      child: child,
    );
    if (onTap == null) return box;
    return Material(
      color: Colors.transparent,
      child: InkWell(borderRadius: BorderRadius.circular(14), onTap: onTap, child: box),
    );
  }
}

/// Small coloured label, e.g. a risk level or status.
class Tag extends StatelessWidget {
  const Tag(this.text, this.color, {super.key});
  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) => Container(
        padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: color.withValues(alpha: 0.5)),
        ),
        child: Text(text, style: TextStyle(color: color, fontSize: 12, fontWeight: FontWeight.w600)),
      );
}

void showMessage(BuildContext context, String message, {bool error = false}) {
  ScaffoldMessenger.of(context)
    ..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(
      content: Text(message),
      backgroundColor: error ? const Color(0xFF3A1020) : Brand.panel,
    ));
}
