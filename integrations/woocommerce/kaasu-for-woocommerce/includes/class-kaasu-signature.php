<?php
if (!defined('ABSPATH')) {
    exit;
}

/**
 * Verifies X-Kaasu-Signature: t=<unix>,v1=<hex HMAC-SHA256(secret, t "." body)>.
 */
final class Kaasu_Signature {
    public static function verify(string $secret, string $header, string $body, int $now, int $tolerance = 300): bool {
        $parts = [];
        foreach (explode(',', $header) as $p) {
            $kv = explode('=', $p, 2);
            if (count($kv) === 2) {
                $parts[$kv[0]] = $kv[1];
            }
        }
        if (!isset($parts['t'], $parts['v1']) || !ctype_digit($parts['t'])) {
            return false;
        }
        $t = (int) $parts['t'];
        if (abs($now - $t) > $tolerance) {
            return false;
        }
        $want = hash_hmac('sha256', $parts['t'] . '.' . $body, $secret);
        return hash_equals($want, $parts['v1']);
    }
}
