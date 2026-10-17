<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/sodium');
header('Content-Type: text/plain');

$key = str_repeat("\x01", SODIUM_CRYPTO_SECRETBOX_KEYBYTES);
$nonce = str_repeat("\x02", SODIUM_CRYPTO_SECRETBOX_NONCEBYTES);
switch ($q['op'] ?? '') {
    case 'secretbox':
        echo bin2hex(sodium_crypto_secretbox('hello', $nonce, $key));
        break;
    case 'secretbox_open':
        $c = sodium_crypto_secretbox('hello', $nonce, $key);
        echo sodium_crypto_secretbox_open($c, $nonce, $key);
        break;
    case 'secretbox_tampered':
        $c = sodium_crypto_secretbox('hello', $nonce, $key);
        $c[0] = chr(ord($c[0]) ^ 1);
        echo var_export(sodium_crypto_secretbox_open($c, $nonce, $key), true);
        break;
    case 'generichash':
        echo bin2hex(sodium_crypto_generichash(''));
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
