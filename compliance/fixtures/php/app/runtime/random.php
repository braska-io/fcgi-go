<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/random');
header('Content-Type: text/plain');

switch ($q['op'] ?? '') {
    case 'bytes':
        echo strlen(random_bytes((int) ($q['n'] ?? 16)));
        break;
    case 'bytes_zero':
        try {
            random_bytes(0);
            echo 'no error';
        } catch (ValueError $e) {
            echo 'ValueError';
        }
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
