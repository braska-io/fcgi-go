<?php
// exit() and die() with string and int status.
//   case=string          exit('bye')
//   case=die             die('bye')
//   case=int             exit(3)
//   case=int_after_echo  echo 'a'; exit(3)
//   case=finally         exit inside try; the finally block must not run
//   case=header          header() before exit('x') still reaches the client
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/lifecycle/exit');

switch ($q['case'] ?? '') {
case 'string':
    exit('bye');
case 'die':
    die('bye');
case 'int':
    exit(3);
case 'int_after_echo':
    echo 'a';
    exit(3);
case 'finally':
    try {
        echo 'T';
        exit;
    } finally {
        echo 'F';
    }
case 'header':
    header('X-Before-Exit: yes');
    exit('x');
default:
    http_response_code(400);
    echo 'unknown case';
}
