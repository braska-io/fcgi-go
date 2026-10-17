<?php
// setcookie(), setrawcookie() and Set-Cookie through header().
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/cookies');

switch ($q['case'] ?? 'read') {
    case 'read':
        echo json_encode($_COOKIE);
        break;
    case 'multi':
        setcookie('a', '1');
        setcookie('b', '2');
        setcookie('c', '3');
        break;
    case 'encode':
        setcookie('enc', 'a;b/c');
        break;
    case 'raw':
        setrawcookie('raw', 'a%3Bb');
        break;
    case 'attrs':
        setcookie('attr', 'v', [
            'path' => '/p',
            'domain' => 'example.test',
            'secure' => true,
            'httponly' => true,
            'samesite' => 'Strict',
        ]);
        break;
    case 'delete':
        setcookie('gone', '', time() - 3600);
        break;
    case 'delete_a':
        setcookie('a', '', time() - 3600);
        break;
    case 'expires_past':
        setcookie('old', 'v', 1);
        break;
    case 'header_replace':
        // header() replaces earlier fields of the same name by default, cookies included.
        setcookie('a', '1');
        header('Set-Cookie: b=2');
        break;
    case 'header_add':
        setcookie('a', '1');
        header('Set-Cookie: hdr=1; Secure; Path=/; Partitioned;', false);
        break;
}
