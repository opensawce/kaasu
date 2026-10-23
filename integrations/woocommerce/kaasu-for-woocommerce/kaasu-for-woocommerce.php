<?php
/**
 * Plugin Name: Kaasu for WooCommerce
 * Description: Accept UPI payments through a self-hosted Kaasu server.
 * Version: 0.1.0
 * Requires PHP: 8.1
 * License: AGPL-3.0-or-later
 */

if (!defined('ABSPATH')) {
    exit;
}

require_once __DIR__ . '/includes/class-kaasu-signature.php';
require_once __DIR__ . '/includes/class-kaasu-amount.php';
require_once __DIR__ . '/includes/class-kaasu-gateway.php';

add_filter('woocommerce_payment_gateways', function (array $gateways): array {
    $gateways[] = 'Kaasu_Gateway';
    return $gateways;
});

add_action('before_woocommerce_init', function () {
    if (class_exists(\Automattic\WooCommerce\Utilities\FeaturesUtil::class)) {
        \Automattic\WooCommerce\Utilities\FeaturesUtil::declare_compatibility('custom_order_tables', __FILE__, true);
    }
});
