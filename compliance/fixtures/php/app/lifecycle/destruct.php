<?php
// Destructors at end of request.
//   case=unset   "A", object destroyed by unset(), "B"   -> "ADB"
//   case=end     object alive until the script ends       -> "MD"
//   case=exit    exit('X') with a live object              -> "XD"
//   case=header  the destructor calls header() at shutdown -> header absent
//   case=throw   the destructor throws at shutdown         -> fatal error
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/lifecycle/destruct');

class D
{
    public function __construct(private string $mode = '')
    {
    }

    public function __destruct()
    {
        if ($this->mode === 'header') {
            header('X-From-Destructor: yes');
        }
        echo 'D';
        if ($this->mode === 'throw') {
            throw new RuntimeException('thrown from destructor');
        }
    }
}

switch ($q['case'] ?? '') {
case 'unset':
    echo 'A';
    $o = new D();
    unset($o);
    echo 'B';
    break;
case 'end':
    $o = new D();
    echo 'M';
    break;
case 'exit':
    $o = new D();
    exit('X');
case 'header':
    $o = new D('header');
    echo 'M';
    break;
case 'throw':
    $o = new D('throw');
    echo 'M';
    break;
default:
    http_response_code(400);
    echo 'unknown case';
}
