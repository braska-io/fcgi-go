<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/ctype');
header('Content-Type: text/plain');

$b = static fn (bool $v): string => $v ? '1' : '0';
switch ($q['op'] ?? '') {
    case 'digit':
        echo $b(ctype_digit('1820.20')), $b(ctype_digit('10002')), $b(ctype_digit('wsl!12')), $b(ctype_digit(''));
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
