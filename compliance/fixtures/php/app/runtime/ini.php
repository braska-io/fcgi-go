<?php
// Reads and changes configuration from php.ini and the pool.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/ini');
header('Content-Type: text/plain');
// Holding the worker lets parallel calls occupy every worker of the pool.
usleep(1000 * (int) ($q['sleep_ms'] ?? 0));

switch ($q['op'] ?? 'get') {
    case 'get':
        echo var_export(ini_get($q['name'] ?? ''), true);
        break;
    case 'display_errors':
        // php.net ini_get: an off boolean reads as "" or "0".
        echo in_array(ini_get('display_errors'), ['', '0'], true) ? 'off' : 'on';
        break;
    case 'set_admin':
        // php_admin_flag in the pool cannot be changed by the script.
        $r = ini_set('display_errors', '1');
        echo var_export($r, true), ',', in_array(ini_get('display_errors'), ['', '0'], true) ? 'off' : 'on';
        break;
    case 'set_memory':
        $old = ini_set('memory_limit', '96M');
        echo var_export($old, true), ',', ini_get('memory_limit');
        break;
    case 'try_set':
        // ini_set on a name the caller may have locked with PHP_ADMIN_VALUE.
        $name = $q['name'] ?? '';
        $r = ini_set($name, $q['value'] ?? '');
        echo var_export($r !== false, true), ',', ini_get($name);
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
