<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/hash');
header('Content-Type: text/plain');

$fox = 'The quick brown fox jumps over the lazy dog';
echo match ($q['op'] ?? '') {
    'sha256' => hash('sha256', 'abc'),
    'hmac' => hash_hmac('sha256', $fox, 'key'),
    'crc32b' => hash('crc32b', $fox),
    'hkdf' => bin2hex(hash_hkdf('sha256', hex2bin('0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b'), 42, hex2bin('f0f1f2f3f4f5f6f7f8f9'), hex2bin('000102030405060708090a0b0c'))),
    default => 'unknown op',
};
