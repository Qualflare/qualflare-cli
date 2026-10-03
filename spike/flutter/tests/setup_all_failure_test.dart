import 'package:flutter_test/flutter_test.dart';

void main() {
  group('with a broken setUpAll', () {
    setUpAll(() => throw StateError('setUpAll exploded'));
    test('never runs', () {});
  });
}
