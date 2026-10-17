<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/gd');

function png2x2(): string
{
    $im = imagecreatetruecolor(2, 2);
    imagesetpixel($im, 0, 0, imagecolorallocate($im, 255, 0, 0));
    imagesetpixel($im, 1, 1, imagecolorallocate($im, 0, 0, 255));
    ob_start();
    imagepng($im);
    return ob_get_clean();
}

switch ($q['op'] ?? '') {
    case 'png_serve':
        header('Content-Type: image/png');
        echo png2x2();
        break;
    case 'png_info':
        header('Content-Type: text/plain');
        $png = png2x2();
        $info = getimagesizefromstring($png);
        echo bin2hex(substr($png, 0, 8)), ',', $info[0], 'x', $info[1], ',', $info[2] === IMAGETYPE_PNG ? 'IMAGETYPE_PNG' : $info[2], ',', $info['mime'];
        break;
    case 'jpeg':
        header('Content-Type: text/plain');
        $im = imagecreatetruecolor(4, 3);
        ob_start();
        imagejpeg($im);
        $jpg = ob_get_clean();
        $info = getimagesizefromstring($jpg);
        echo bin2hex(substr($jpg, 0, 3)), ',', $info[0], 'x', $info[1], ',', $info['mime'];
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
