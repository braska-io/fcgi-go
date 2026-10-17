<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/json');
header('Content-Type: text/plain; charset=UTF-8');

switch ($q['op'] ?? '') {
    case 'pretty':
        echo json_encode(['a' => 1, 'b' => [1, 2], 'c' => []], JSON_PRETTY_PRINT);
        break;
    case 'unicode':
        echo json_encode("\u{00FC}/"), ' ', json_encode("\u{00FC}/", JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
        break;
    case 'bigint':
        $o = json_decode('{"n":12345678901234567890}', false, 512, JSON_BIGINT_AS_STRING);
        $f = json_decode('12345678901234567890');
        echo var_export($o->n, true), ',', gettype($f);
        break;
    case 'decode_error':
        echo var_export(json_decode('{bad'), true), ',', json_last_error(), ',', json_last_error_msg();
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
