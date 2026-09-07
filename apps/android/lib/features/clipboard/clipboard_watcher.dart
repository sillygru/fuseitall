// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Deprecated: auto clipboard watching removed. This stub remains for
// import compatibility; new code should not use it.

import 'package:flutter/services.dart';

class ClipboardWatcher {
  ClipboardWatcher({EventChannel? channel}) : _channel = channel ?? const EventChannel('fuseitall/clipboardEvents');
  final EventChannel _channel;
  Stream<String> get changes => const Stream.empty();
}
