<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/openssl');
header('Content-Type: text/plain');

switch ($q['op'] ?? '') {
    case 'aes_ecb_vector':
        // FIPS-197 appendix C.1: one AES-128 block, no padding.
        $c = openssl_encrypt(hex2bin('00112233445566778899aabbccddeeff'), 'aes-128-ecb',
            hex2bin('000102030405060708090a0b0c0d0e0f'), OPENSSL_RAW_DATA | OPENSSL_ZERO_PADDING);
        echo bin2hex($c);
        break;
    case 'aes_cbc':
        $key = str_repeat("\x01", 32);
        $iv = str_repeat("\x02", 16);
        echo openssl_encrypt('hello', 'aes-256-cbc', $key, 0, $iv);
        break;
    case 'roundtrip':
        $key = str_repeat("\x01", 32);
        $iv = str_repeat("\x02", 12);
        $tag = '';
        $c = openssl_encrypt('hello gcm', 'aes-256-gcm', $key, OPENSSL_RAW_DATA, $iv, $tag);
        $p = openssl_decrypt($c, 'aes-256-gcm', $key, OPENSSL_RAW_DATA, $iv, $tag);
        $tag[0] = chr(ord($tag[0]) ^ 1);
        $bad = openssl_decrypt($c, 'aes-256-gcm', $key, OPENSSL_RAW_DATA, $iv, $tag);
        echo $p, ',', strlen($c), ',', var_export($bad, true);
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
