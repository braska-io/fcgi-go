<?php
// memory_limit (64M from php.ini).
//   case=ini           ini_get('memory_limit')
//   case=under         a 40 MiB string fits
//   case=exhaust&k=K   a 100 MiB string does not; a shutdown function marks
//   case=lower         ini_set('memory_limit', '32M'), then 40 MiB
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/../pdo/_db.php';
$q = fixture('app/lifecycle/memory');
$k = $q['k'] ?? '';

switch ($q['case'] ?? '') {
case 'ini':
    echo ini_get('memory_limit');
    break;
case 'under':
    $s = str_repeat('x', 40 << 20);
    echo strlen($s);
    break;
case 'exhaust':
    register_shutdown_function(function () use ($k) {
        mark($k, 'shutdown status=' . connection_status());
    });
    echo 'start';
    $s = str_repeat('x', 100 << 20);
    echo strlen($s);
    break;
case 'lower':
    echo 'old=' . ini_set('memory_limit', '32M') . ' ';
    $s = str_repeat('x', 40 << 20);
    echo strlen($s);
    break;
default:
    http_response_code(400);
    echo 'unknown case';
}
