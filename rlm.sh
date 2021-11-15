mkdir "/mnt/root" -p
mount -o loop qt-rootfs.img /mnt/root
cp -f ./修改/lm /mnt/root/usr/share/demo/lm
umount /mnt/root
echo "操作完成"