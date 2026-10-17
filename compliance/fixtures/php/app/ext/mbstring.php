<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/mbstring');
header('Content-Type: text/plain; charset=UTF-8');

$s = "Gr\u{00FC}\u{00DF}e"; // "Grüße": 5 characters, 7 bytes in UTF-8

echo match ($q['op'] ?? '') {
    'strlen' => strlen($s) . ',' . mb_strlen($s) . ',' . mb_strlen($s, '8bit'),
    'upper' => mb_convert_case("stra\u{00DF}e", MB_CASE_UPPER, 'UTF-8'),
    'upper_simple' => mb_convert_case("stra\u{00DF}e", MB_CASE_UPPER_SIMPLE, 'UTF-8'),
    'substr' => mb_substr($s, 2, 2) . '|' . bin2hex(substr($s, 2, 2)),
    'detect' => mb_detect_encoding($s, ['ASCII', 'UTF-8', 'ISO-8859-1'], true)
        . ',' . mb_detect_encoding('abc', ['ASCII', 'UTF-8'], true)
        . ',' . var_export(mb_detect_encoding("\xFF\xFE\xFD", ['ASCII', 'UTF-8'], true), true),
    default => (function (): string {
        http_response_code(400);
        return 'unknown op';
    })(),
};
