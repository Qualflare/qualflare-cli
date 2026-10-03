import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

var flakyAttempts = 0;

void main() {
  group('login screen', () {
    testWidgets('shows a title', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: Text('Sign in')));
      expect(find.text('Sign in'), findsOneWidget);
    });

    testWidgets('fails an expectation', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: Text('Sign in')));
      expect(find.text('Sign out'), findsOneWidget);
    });

    testWidgets('throws an error', (tester) async {
      throw StateError('boom');
    });

    testWidgets('is skipped', (tester) async {}, skip: true);

    testWidgets('prints output', (tester) async {
      // ignore: avoid_print
      print('hello from a widget test');
    });

    test('passes on the third attempt', () {
      flakyAttempts++;
      expect(flakyAttempts, 3);
    }, retry: 2);
  });
}
