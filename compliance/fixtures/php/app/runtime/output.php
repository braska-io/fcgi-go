<?php
// Output buffering, late headers and fastcgi_finish_request().
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/output');
header('Content-Type: text/plain');

switch ($q['op'] ?? '') {
    case 'late_header':
        // Below output_buffering=4096 the headers are not sent yet.
        echo 'x';
        echo var_export(headers_sent(), true);
        header('X-Late: 1');
        break;
    case 'header_after_overflow':
        echo str_repeat('a', 8192);
        $sent = headers_sent();
        @header('X-Late: 1');
        echo $sent ? 'sent' : 'not sent';
        break;
    case 'finish_request':
        echo 'done';
        $r = fastcgi_finish_request();
        // Output after the call never reaches the client.
        echo 'after';
        sleep(2);
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
