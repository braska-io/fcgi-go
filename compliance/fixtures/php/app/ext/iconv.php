<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/iconv');
header('Content-Type: text/plain; charset=UTF-8');

$s = "Gr\u{00FC}\u{00DF}e";
switch ($q['op'] ?? '') {
    case 'latin1':
        echo bin2hex(iconv('UTF-8', 'ISO-8859-1', $s));
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
