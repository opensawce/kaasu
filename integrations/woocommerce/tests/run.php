<?php
// Plain PHP tests, no WordPress needed. Run: php integrations/woocommerce/tests/run.php
define('ABSPATH', __DIR__);
require __DIR__ . '/../kaasu-for-woocommerce/includes/class-kaasu-amount.php';
require __DIR__ . '/../kaasu-for-woocommerce/includes/class-kaasu-signature.php';

$failures = 0;
function expect($label, $got, $want) {
    global $failures;
    if ($got !== $want) {
        $failures++;
        echo "FAIL $label: got " . var_export($got, true) . " want " . var_export($want, true) . "\n";
    }
}

// Amounts: integer paise, never floats.
expect('499.07', Kaasu_Amount::to_paise('499.07'), 49907);
expect('0.29', Kaasu_Amount::to_paise('0.29'), 29);
expect('1200.00', Kaasu_Amount::to_paise('1200.00'), 120000);
expect('5', Kaasu_Amount::to_paise('5'), 500);
foreach (['', '-5', '1,200.00', '1.234', 'abc', '0', '0.00'] as $bad) {
    try { Kaasu_Amount::to_paise($bad); expect("reject $bad", 'accepted', 'rejected'); }
    catch (InvalidArgumentException $e) { }
}

// Signatures, the same vector the Go verifier uses.
$secret = 'whsec_test_example';
$body = '{"event_id":"evt_example","type":"order.confirmed","data":{"order":{"id":"ord_1","state":"confirmed","amount_paise":49907}}}';
$header = 't=1767261600,v1=' . hash_hmac('sha256', '1767261600.' . $body, $secret);
expect('genuine', Kaasu_Signature::verify($secret, $header, $body, 1767261600), true);
expect('tampered', Kaasu_Signature::verify($secret, $header, $body . ' ', 1767261600), false);
expect('stale', Kaasu_Signature::verify($secret, $header, $body, 1767261600 + 600), false);
expect('wrong secret', Kaasu_Signature::verify('other', $header, $body, 1767261600), false);
expect('garbage', Kaasu_Signature::verify($secret, 'nope', $body, 1767261600), false);

if ($failures) { exit(1); }
echo "all woocommerce checks pass\n";
