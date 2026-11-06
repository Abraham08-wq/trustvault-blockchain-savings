import 'package:flutter/material.dart';

import '../theme.dart';

/// The ChamaChain shield, drawn in code so the app needs no image files.
class ShieldMark extends StatelessWidget {
  const ShieldMark({super.key, this.size = 40});
  final double size;

  @override
  Widget build(BuildContext context) => Align(
        alignment: Alignment.centerLeft,
        child: CustomPaint(size: Size(size * 0.92, size), painter: _ShieldPainter()),
      );
}

class _ShieldPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size s) {
    final w = s.width, h = s.height;
    final shield = Path()
      ..moveTo(w * 0.5, 0)
      ..lineTo(w * 0.04, h * 0.15)
      ..lineTo(w * 0.04, h * 0.46)
      ..cubicTo(w * 0.04, h * 0.72, w * 0.24, h * 0.92, w * 0.5, h)
      ..cubicTo(w * 0.76, h * 0.92, w * 0.96, h * 0.72, w * 0.96, h * 0.46)
      ..lineTo(w * 0.96, h * 0.15)
      ..close();
    canvas.drawPath(
      shield,
      Paint()
        ..color = Brand.signal
        ..style = PaintingStyle.stroke
        ..strokeWidth = s.width * 0.07
        ..strokeJoin = StrokeJoin.round,
    );
    final tick = Path()
      ..moveTo(w * 0.28, h * 0.5)
      ..lineTo(w * 0.44, h * 0.64)
      ..lineTo(w * 0.73, h * 0.36);
    canvas.drawPath(
      tick,
      Paint()
        ..color = Brand.safe
        ..style = PaintingStyle.stroke
        ..strokeWidth = s.width * 0.08
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round,
    );
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
