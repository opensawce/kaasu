<?php
if (!defined('ABSPATH')) {
    exit;
}

/**
 * Converts a WooCommerce order total into integer paise without floats.
 */
final class Kaasu_Amount {
    /**
     * @param string $total e.g. "499.07", "1200.00", "0.29" (WooCommerce decimal string)
     * @return int paise, or throws on anything that is not a plain decimal
     */
    public static function to_paise(string $total): int {
        $total = trim($total);
        if (!preg_match('/^\d+(\.\d{1,2})?$/', $total)) {
            throw new InvalidArgumentException('amount is not a plain decimal');
        }
        [$rupees, $frac] = array_pad(explode('.', $total, 2), 2, '');
        $frac = str_pad($frac, 2, '0');
        $paise = ((int) $rupees) * 100 + (int) $frac;
        if ($paise <= 0) {
            throw new InvalidArgumentException('amount must be positive');
        }
        return $paise;
    }
}
