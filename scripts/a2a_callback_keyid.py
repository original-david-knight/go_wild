#!/usr/bin/env python3
"""
Derive or generate A2A callback key material.

Usage:
  1) Derive callback key ID (manager allowlist value):
       A2A_CALLBACK_SIGNING_KEY=... ./scripts/a2a_callback_keyid.py
     or
       ./scripts/a2a_callback_keyid.py --key <encoded_key>

  2) Generate a new signing key and matching key ID:
       ./scripts/a2a_callback_keyid.py --generate --format env
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import sys
from typing import Iterable

try:
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import ed25519
except Exception as exc:  # pragma: no cover - runtime dependency check
    sys.stderr.write(
        "error: missing dependency 'cryptography'. "
        "Install with: pip install cryptography\n"
    )
    raise SystemExit(2) from exc


ENV_SIGNING_KEY = "A2A_CALLBACK_SIGNING_KEY"
ENV_ALLOWED_IDS = "A2A_CALLBACK_ALLOWED_KEY_IDS"


def _decode_candidates(raw: str) -> Iterable[bytes]:
    trimmed = raw.strip()
    if not trimmed:
        return []

    out: list[bytes] = []

    # base64url (unpadded or padded)
    try:
        padded = trimmed + ("=" * (-len(trimmed) % 4))
        out.append(base64.urlsafe_b64decode(padded.encode("ascii")))
    except Exception:
        pass

    # base64 standard
    try:
        out.append(base64.b64decode(trimmed.encode("ascii"), validate=True))
    except Exception:
        pass

    # hex
    try:
        out.append(bytes.fromhex(trimmed))
    except Exception:
        pass

    return out


def _signing_key_from_encoded(raw: str) -> ed25519.Ed25519PrivateKey:
    for candidate in _decode_candidates(raw):
        if len(candidate) == 32:
            seed = candidate
        elif len(candidate) == 64:
            # Go ed25519 private key encoding is 64 bytes: seed + public key.
            seed = candidate[:32]
        else:
            continue
        try:
            return ed25519.Ed25519PrivateKey.from_private_bytes(seed)
        except ValueError:
            continue
    raise ValueError(
        "signing key must be base64url/base64/hex encoded Ed25519 "
        "32-byte seed or 64-byte private key"
    )


def _key_id_from_private_key(key: ed25519.Ed25519PrivateKey) -> str:
    pub = key.public_key().public_bytes(
        encoding=serialization.Encoding.Raw,
        format=serialization.PublicFormat.Raw,
    )
    return base64.urlsafe_b64encode(pub).rstrip(b"=").decode("ascii")


def _generate_seed_hex() -> str:
    return os.urandom(32).hex()


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Derive A2A callback key ID from A2A_CALLBACK_SIGNING_KEY, "
            "or generate a new key pair."
        )
    )
    parser.add_argument(
        "--key",
        help=(
            "Encoded signing key value. If omitted, reads from "
            f"${ENV_SIGNING_KEY}."
        ),
    )
    parser.add_argument(
        "--generate",
        action="store_true",
        help="Generate a new signing key (hex seed) and matching key ID.",
    )
    parser.add_argument(
        "--format",
        choices=("plain", "env", "json"),
        default="plain",
        help=(
            "Output format: plain (default), env (shell assignments), "
            "or json."
        ),
    )
    return parser.parse_args()


def _print_result(signing_key: str, key_id: str, output_format: str, generated: bool) -> None:
    if output_format == "json":
        payload = {
            ENV_SIGNING_KEY: signing_key if generated else None,
            ENV_ALLOWED_IDS: key_id,
        }
        if not generated:
            payload.pop(ENV_SIGNING_KEY)
        print(json.dumps(payload))
        return

    if output_format == "env":
        if generated:
            print(f"{ENV_SIGNING_KEY}={signing_key}")
        print(f"{ENV_ALLOWED_IDS}={key_id}")
        return

    # plain
    if generated:
        print(f"signing_key={signing_key}")
    print(f"key_id={key_id}")


def main() -> int:
    args = _parse_args()

    if args.generate:
        signing_key = _generate_seed_hex()
    else:
        signing_key = (args.key or os.getenv(ENV_SIGNING_KEY, "")).strip()
        if not signing_key:
            sys.stderr.write(
                "error: missing signing key. "
                f"Set {ENV_SIGNING_KEY} or pass --key.\n"
            )
            return 2

    try:
        priv = _signing_key_from_encoded(signing_key)
    except ValueError as err:
        sys.stderr.write(f"error: {err}\n")
        return 2

    key_id = _key_id_from_private_key(priv)
    _print_result(signing_key, key_id, args.format, generated=args.generate)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
