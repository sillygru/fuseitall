// SPDX-License-Identifier: AGPL-3.0-only

// Either-style result: Ok(value) or Err(failure). No dartz dependency
// (kept out per minimal-deps rule); this 20-line sealed type covers it.
sealed class Result<T> {
  const Result();
}

/// Successful outcome carrying [value].
final class Ok<T> extends Result<T> {
  const Ok(this.value);
  final T value;
}

/// Failed outcome carrying [failure]. Never throw for expected errors.
final class Err<T> extends Result<T> {
  const Err(this.failure);
  final Failure failure;
}

/// Typed failures; every UI error renders [message] via SelectableText.rich.
sealed class Failure {
  const Failure(this.message);
  final String message;
}

/// Malformed QR JSON, bad ping/pong shape, or unexpected HTTP status.
final class ParseFailure extends Failure {
  const ParseFailure(super.message);
}

/// QR or wire version newer than this build speaks. Shown as update screen.
final class UnsupportedVersion extends Failure {
  const UnsupportedVersion(this.version, {this.maxSupported = 1})
      : super(
          'Update FuseItAll on this device to a build that speaks QR v$version.',
        );
  final int version;
  final int maxSupported;
}

/// HTTP 403: the Mac rejected our pairing token.
final class AuthFailure extends Failure {
  const AuthFailure(super.message);
}

/// HTTP 426 / error+UPDATE_REQUIRED: [message] is the server's verbatim text
/// (`Update FuseItAll on device to requiredVersion; current
/// currentVersion`). Version fields may be empty from older peers; the UI
/// falls back to the verbatim message. Branch on this type, never on strings.
final class UpdateRequired extends Failure {
  const UpdateRequired(
    super.message, {
    this.requiredVersion = '',
    this.currentVersion = '',
    this.requiredBuild = 0,
    this.device = '',
  });
  final String requiredVersion;
  final String currentVersion;
  final int requiredBuild;
  final String device;
}

/// Transport-level failure (TLS, timeout, unreachable host).
final class NetworkFailure extends Failure {
  const NetworkFailure(super.message);
}

/// Pong nonce did not echo the ping nonce. Fail closed.
final class NonceMismatch extends Failure {
  const NonceMismatch(super.message);
}
