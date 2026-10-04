import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_project/main.dart';

// Counts attempts within one process so the retried test fails once, then passes.
int _attempts = 0;

void main() {
  testWidgets('shows a welcome message', (tester) async {
    await tester.pumpWidget(const MyApp());
    expect(find.text('Welcome'), findsOneWidget);
  });

  // Fails on purpose: the CI job asserts the parser reports exactly one failure.
  testWidgets('fails an expectation', (tester) async {
    await tester.pumpWidget(const MyApp());
    expect(find.text('Sign out'), findsOneWidget);
  });

  testWidgets('is skipped', (tester) async {
    await tester.pumpWidget(const MyApp());
    expect(find.text('Welcome'), findsOneWidget);
  }, skip: true);

  test('passes on the second attempt', () {
    _attempts++;
    expect(_attempts, 2);
  }, retry: 1);
}
