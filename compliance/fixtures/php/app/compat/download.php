<?php
// Download responses.
//   case=utf8   Content-Disposition with a raw UTF-8 filename, small PDF-like body
//   case=short  declares Content-Length 1000, sends 10 bytes, exits
//   case=big    3 MiB pattern with an exact Content-Length after clearing
//               every output buffer, as php.net readfile() shows
//   case=plain  36 bytes of text, no Range handling in the script
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/compat/download');

switch ($q['case'] ?? '') {
case 'utf8':
    header('Content-Type: application/pdf');
    header('Content-Disposition: attachment; filename="Prüfbericht_ä.pdf"');
    echo "%PDF-1.4\n";
    break;
case 'short':
    header('Content-Type: application/octet-stream');
    header('Content-Length: 1000');
    echo '0123456789';
    exit;
case 'big':
    $size = 3145728;
    header('Content-Type: application/octet-stream');
    header('Content-Length: ' . $size);
    while (ob_get_level() > 0) {
        ob_end_clean();
    }
    $block = str_repeat(pattern(256), 256); // 64 KiB, keeps the pattern aligned
    for ($sent = 0; $sent < $size; $sent += strlen($block)) {
        echo $block;
    }
    flush();
    break;
case 'plain':
    header('Content-Type: text/plain; charset=utf-8');
    echo '0123456789abcdefghijklmnopqrstuvwxyz';
    break;
default:
    http_response_code(400);
    echo 'unknown case';
}
