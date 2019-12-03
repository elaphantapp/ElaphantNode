#!/usr/bin/env bash
#
# install.sh is a script to easily fetch and install new elaphant node releases
#
# Home: https://github.com/elaphantapp/elaphantnode
#
# AUTHOR xiaomingfuckeasylife

me=$(basename "$0")
msg() {
    echo >&2 "$me": "$*"
}

# defaults
arch_probe="uname -m"
arch=$($arch_probe)
case "$arch" in
    i*)
        msg 386 is not supported, using amd64
        exit 0
        ;;
    x*)
        arch=amd64
        ;;
    aarch64)
        #arch=armv6l
        msg arm64 is not supported, using amd64
        exit 0
        ;;
    armv7l)
        # Go project does not provide a binary release for armv71
        msg armv7l is not supported, using amd64
        exit 0
        ;;
esac

os_probe="uname"
os=$($os_probe)
case "$os" in
    Linux)
        ;;
    *)
        msg $os system is not supported
        exit 0
        ;;
esac

msg we are going to create a sudo user for elaphant node , please enter the user name
read user
apt-get install build-essential
adduser $user
usermod -aG sudo $user
su - $user
sh -c "$(curl -fsSL https://raw.githubusercontent.com/Linuxbrew/install/master/install.sh)"
echo 'eval $(/home/linuxbrew/.linuxbrew/bin/brew shellenv)' >>~/.profile
source ~/.profile
brew tap elaphantapp/elaphant
brew install elaphant
echo '{"Configuration":{"ActiveNet":"mainnet","HttpInfoPort":20333,"HttpInfoStart":true,"HttpRestPort":20334,"HttpRestStart":true,"HttpWsPort":20335,"HttpWsStart":true,"HttpJsonPort":20336,"EnableRPC":true,"NodePort":20338,"PrintLevel":1,"PowConfiguration":{"PayToAddr":"EeEkSiRMZqg5rd9a2yPaWnvdPcikFtsrjE","MinerInfo":"ELA","MinTxFee":100},"RpcConfiguration":{"User":"clark","Pass":"123456","WhiteIPList":["127.0.0.1"]}}}' > config.json
echo '{"EarnReward":true,"BundleUtxoSize":700}' > ext_config.json
msg congratulations!
exit 0