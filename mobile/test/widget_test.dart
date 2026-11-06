import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:chamachain_member/theme.dart';

void main() {
  testWidgets('panel and tag render their content', (tester) async {
    await tester.pumpWidget(MaterialApp(
      theme: buildTheme(),
      home: const Scaffold(
        body: Panel(child: Tag('High risk', Brand.alert)),
      ),
    ));
    expect(find.text('High risk'), findsOneWidget);
  });
}
