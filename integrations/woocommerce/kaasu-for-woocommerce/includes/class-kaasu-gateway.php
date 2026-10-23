<?php
if (!defined('ABSPATH')) {
    exit;
}

/**
 * The payment method. Creates a Kaasu order, sends the shopper to the pay page, and marks the WooCommerce
 * order paid only when a verified "order.confirmed" webhook arrives.
 */
final class Kaasu_Gateway extends WC_Payment_Gateway {
    public function __construct() {
        $this->id = 'kaasu';
        $this->method_title = 'Kaasu';
        $this->method_description = 'UPI payments confirmed by your self-hosted Kaasu server.';
        $this->has_fields = false;
        $this->supports = ['products'];
        $this->init_form_fields();
        $this->init_settings();
        $this->title = $this->get_option('title', 'UPI (Kaasu)');
        $this->enabled = $this->get_option('enabled', 'no');
        add_action('woocommerce_update_options_payment_gateways_' . $this->id, [$this, 'process_admin_options']);
        add_action('woocommerce_api_kaasu', [$this, 'handle_webhook']);
    }

    public function init_form_fields(): void {
        $this->form_fields = [
            'enabled' => ['title' => 'Enable', 'type' => 'checkbox', 'default' => 'no'],
            'title' => ['title' => 'Title', 'type' => 'text', 'default' => 'UPI (Kaasu)'],
            'kaasu_url' => ['title' => 'Kaasu URL', 'type' => 'text', 'description' => 'https://pay.example.com'],
            'api_key' => ['title' => 'API key', 'type' => 'password'],
            'webhook_secret' => ['title' => 'Webhook secret', 'type' => 'password'],
        ];
    }

    public function process_payment($order_id): array {
        $order = wc_get_order($order_id);
        $amount = Kaasu_Amount::to_paise((string) $order->get_total());
        $response = wp_remote_post(rtrim($this->get_option('kaasu_url'), '/') . '/v1/orders', [
            'timeout' => 15,
            'headers' => [
                'Authorization' => 'Bearer ' . $this->get_option('api_key'),
                'Content-Type' => 'application/json',
                // Stable per order, so a retry never creates a second payment.
                'Idempotency-Key' => 'wc-' . $order_id . '-' . $order->get_order_key(),
            ],
            'body' => wp_json_encode(['amount_paise' => $amount, 'reference' => (string) $order->get_order_number()]),
        ]);
        if (is_wp_error($response) || wp_remote_retrieve_response_code($response) !== 201) {
            wc_add_notice('Payment could not be started. Please try again.', 'error');
            return ['result' => 'failure'];
        }
        $data = json_decode(wp_remote_retrieve_body($response), true);
        $order->update_meta_data('_kaasu_order_id', $data['id'] ?? '');
        $order->save();
        return [
            'result' => 'success',
            'redirect' => rtrim($this->get_option('kaasu_url'), '/') . '/pay/' . rawurlencode((string) ($data['id'] ?? '')),
        ];
    }

    public function handle_webhook(): void {
        $body = file_get_contents('php://input');
        $sig = $_SERVER['HTTP_X_KAASU_SIGNATURE'] ?? '';
        if (!Kaasu_Signature::verify((string) $this->get_option('webhook_secret'), $sig, $body, time())) {
            status_header(401);
            exit;
        }
        $event = json_decode($body, true);
        $event_id = (string) ($event['event_id'] ?? '');
        if ($event_id === '' || get_transient('kaasu_evt_' . $event_id)) {
            status_header(200);
            exit;
        }
        set_transient('kaasu_evt_' . $event_id, 1, DAY_IN_SECONDS);
        if (($event['type'] ?? '') === 'order.confirmed') {
            $kaasu_id = (string) ($event['data']['order']['id'] ?? '');
            $order = $this->find_order($kaasu_id);
            if ($order && !$order->is_paid()) {
                $order->payment_complete($kaasu_id);
            }
        }
        status_header(200);
        exit;
    }

    private function find_order(string $kaasu_id): ?WC_Order {
        $found = wc_get_orders(['meta_key' => '_kaasu_order_id', 'meta_value' => $kaasu_id, 'limit' => 1]);
        return $found[0] ?? null;
    }
}
