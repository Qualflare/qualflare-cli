import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  group('counter app', () {
    testWidgets('starts at zero', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: Text('0')));
      expect(find.text('0'), findsOneWidget);
    });

    testWidgets('fails on device', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: Text('0')));
      expect(find.text('1'), findsOneWidget);
    });
  });
}
