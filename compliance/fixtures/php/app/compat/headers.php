<?php
// Response header shapes applications produce.
//   case=dup  two Cache-Control via replace=false, three Set-Cookie, and a
//             Content-Type written with two spaces after the colon
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/compat/headers');

switch ($q['case'] ?? '') {
case 'dup':
    header('Cache-Control: no-store');
    header('Cache-Control: no-cache', false);
    header('Set-Cookie: a=1', false);
    header('Set-Cookie: b=2', false);
    header('Set-Cookie: c=3', false);
    header('Content-Type:  text/plain; charset=utf-8');
    echo 'dup';
    break;
default:
    http_response_code(400);
    echo 'unknown case';
}
