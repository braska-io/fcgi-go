<?php
// Stores and reads typed session values.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/data');

session_start();
switch ($q['op'] ?? 'get') {
    case 'set':
        $_SESSION[$q['k']] = $q['v'];
        echo 'ok';
        break;
    case 'set_typed':
        $_SESSION['typed'] = ['int' => 42, 'float' => 1.5, 'bool' => true, 'null' => null, 'list' => [1, 2, 3], 'str' => "a\"b\\c\nd"];
        echo 'ok';
        break;
    case 'get':
        echo $_SESSION[$q['k']] ?? '(unset)';
        break;
    case 'dump':
        echo json_encode($_SESSION);
        break;
}
